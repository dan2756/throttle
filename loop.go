package main

import (
	"bufio"
	"io"
	"os"
	"time"
)

// lineSource reads lines on its own goroutine so the main loop can wait on
// input and timers at the same time.
type lineSource struct {
	lines chan []byte
	done  chan struct{}
	err   error // read error other than EOF; valid once lines is closed
}

func readLines(r io.Reader) *lineSource {
	s := &lineSource{lines: make(chan []byte, 256), done: make(chan struct{})}
	go s.read(r)
	return s
}

func (s *lineSource) read(r io.Reader) {
	defer close(s.lines)
	// ReadBytes grows its result as needed, so lines of any length come
	// through whole; the buffer size only affects how much is read at once.
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				s.err = err
			}
			return
		}
		select {
		case s.lines <- line:
		case <-s.done:
			return
		}
	}
}

// stop releases the reader goroutine if it is blocked handing over a line.
func (s *lineSource) stop() { close(s.done) }

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
// line has been released, or as soon as a signal arrives on stop, in which
// case it returns that signal.
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
