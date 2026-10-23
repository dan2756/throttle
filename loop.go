package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// errStopped tells the line reader not to pull more bytes. It is internal:
// a flush was requested, not a failure of the input.
var errStopped = errors.New("stop")

// lineSource reads lines on its own goroutine so the main loop can wait on
// input and timers at the same time.
type lineSource struct {
	lines    chan []byte
	done     chan struct{} // closed when the consumer has left
	flushReq chan struct{} // closed to deliver buffered lines and stop reading
	err      error         // read error other than EOF; valid once lines is closed

	mu      sync.Mutex
	reading bool          // true while blocked in the underlying Read
	state   chan struct{} // closed and replaced when reading changes, or the goroutine exits
}

func readLines(r io.Reader) *lineSource {
	s := &lineSource{
		lines:    make(chan []byte, 256),
		done:     make(chan struct{}),
		flushReq: make(chan struct{}),
		state:    make(chan struct{}),
	}
	go s.read(r)
	return s
}

func (s *lineSource) read(r io.Reader) {
	defer s.wake()
	defer close(s.lines)
	// ReadBytes grows its result as needed, so lines of any length come
	// through whole; the buffer size only affects how much is read at once.
	br := bufio.NewReaderSize(&stopReader{r: r, src: s}, 64<<10)
	for {
		if s.flushRequested() {
			s.flushBuffered(br, nil)
			return
		}
		// ReadBytes returns what it has along with the error, which at EOF
		// is a final line that has no newline. Hand that over too.
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && !s.handoff(br, line) {
			return
		}
		if err != nil {
			if errors.Is(err, errStopped) {
				s.flushBuffered(br, nil)
				return
			}
			if err != io.EOF {
				s.err = err
			}
			return
		}
	}
}

// handoff sends line, or, if a flush was requested, line plus anything
// already buffered. It reports false when the reader should exit.
func (s *lineSource) handoff(br *bufio.Reader, line []byte) bool {
	select {
	case s.lines <- line:
		return true
	case <-s.flushReq:
		s.flushBuffered(br, line)
		return false
	case <-s.done:
		return false
	}
}

// flushBuffered delivers pending and every complete line already sitting in
// br, without reading more from the input. A trailing partial line is
// delivered too: those bytes have already been read.
func (s *lineSource) flushBuffered(br *bufio.Reader, pending []byte) {
	if len(pending) > 0 && !s.send(pending) {
		return
	}
	for br.Buffered() > 0 {
		peek, err := br.Peek(br.Buffered())
		if err != nil || len(peek) == 0 {
			return
		}
		n := len(peek)
		if i := bytes.IndexByte(peek, '\n'); i >= 0 {
			n = i + 1
		}
		line := make([]byte, n)
		if _, err := io.ReadFull(br, line); err != nil {
			return
		}
		if !s.send(line) {
			return
		}
		if line[len(line)-1] != '\n' {
			return
		}
	}
}

func (s *lineSource) send(line []byte) bool {
	select {
	case s.lines <- line:
		return true
	case <-s.done:
		return false
	}
}

// stop releases the reader goroutine if it is blocked handing over a line.
func (s *lineSource) stop() { close(s.done) }

// requestFlush asks the reader to hand over lines it has already read and
// not to read any more.
func (s *lineSource) requestFlush() { close(s.flushReq) }

func (s *lineSource) flushRequested() bool {
	select {
	case <-s.flushReq:
		return true
	default:
		return false
	}
}

func (s *lineSource) setReading(v bool) {
	s.mu.Lock()
	s.reading = v
	s.wakeLocked()
	s.mu.Unlock()
}

func (s *lineSource) wake() {
	s.mu.Lock()
	s.wakeLocked()
	s.mu.Unlock()
}

func (s *lineSource) wakeLocked() {
	close(s.state)
	s.state = make(chan struct{})
}

func (s *lineSource) snapshot() (reading bool, state <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reading, s.state
}

// drain delivers lines already taken from the input, then returns when the
// reader is blocked waiting for more or has exited.
func (s *lineSource) drain(t *throttler) error {
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return s.err
			}
			if err := t.Line(line); err != nil {
				return err
			}
		default:
			reading, state := s.snapshot()
			if reading {
				// Blocked on new input, so every complete line already read
				// has been queued. Take those, then stop; waiting for the
				// Read would hang on a quiet stdin.
				select {
				case line, ok := <-s.lines:
					if !ok {
						return s.err
					}
					if err := t.Line(line); err != nil {
						return err
					}
				default:
					return nil
				}
				continue
			}
			select {
			case line, ok := <-s.lines:
				if !ok {
					return s.err
				}
				if err := t.Line(line); err != nil {
					return err
				}
			case <-state:
			}
		}
	}
}

// stopReader fails Read once a flush has been requested, and publishes
// whether a Read is in progress so drain can tell "blocked on new input"
// from "still handing over bytes it already has".
type stopReader struct {
	r   io.Reader
	src *lineSource
}

func (r *stopReader) Read(p []byte) (int, error) {
	if r.src.flushRequested() {
		return 0, errStopped
	}
	r.src.setReading(true)
	defer r.src.setReading(false)
	if r.src.flushRequested() {
		return 0, errStopped
	}
	n, err := r.r.Read(p)
	if r.src.flushRequested() {
		// This Read was already in flight. Those bytes were not sitting in
		// the buffer when the flush was requested; leave them unread by
		// the line splitter.
		return 0, errStopped
	}
	return n, err
}

// alarm is a timer that is only re-armed when the deadline changes.
type alarm struct {
	timer *time.Timer
	at    time.Time
}

// wait returns a channel that fires at the given time, or nil when ok is
// false (a nil channel never fires).
func (a *alarm) wait(at time.Time, ok bool, now time.Time) <-chan time.Time {
	if !ok {
		return nil
	}
	if a.timer == nil {
		a.timer = time.NewTimer(at.Sub(now))
	} else if !at.Equal(a.at) {
		a.timer.Reset(at.Sub(now))
	}
	a.at = at
	return a.timer.C
}

// fired notes that the timer went off, so the next wait re-arms it.
func (a *alarm) fired() { a.at = time.Time{} }

func (a *alarm) stop() {
	if a.timer != nil {
		a.timer.Stop()
	}
}

// pump feeds lines from r through t, running t's Tick whenever its
// deadline passes. It returns once the input has ended and every queued
// line has been released, or when a signal arrives on stop. A signal first
// passes on every line already read, including lines the reader has buffered
// but not yet handed over, and then returns that signal.
func pump(t *throttler, r io.Reader, stop <-chan os.Signal) (os.Signal, error) {
	src := readLines(r)
	defer src.stop()
	in := src.lines

	var timer alarm
	defer timer.stop()

	for {
		if in == nil && t.Queued() == 0 {
			return nil, nil
		}

		// Lines that have already been read go ahead of timers and
		// signals, so an interrupt accounts for everything read before it.
		// (Line catches up on any window or release that is overdue.)
		var (
			line   []byte
			ok     bool
			waited bool
		)
		select {
		case line, ok = <-in:
		default:
			waited = true
		}
		if waited {
			at, due := t.Deadline()
			select {
			case line, ok = <-in:
			case <-timer.wait(at, due, t.clock.Now()):
				timer.fired()
				if err := t.Tick(); err != nil {
					return nil, err
				}
				continue
			case sig := <-stop:
				if in != nil {
					src.requestFlush()
					if err := src.drain(t); err != nil {
						return nil, err
					}
				}
				return sig, nil
			}
		}

		if !ok {
			if src.err != nil {
				return nil, src.err
			}
			in = nil // keep going until the queue drains
			continue
		}
		if err := t.Line(line); err != nil {
			return nil, err
		}
	}
}
