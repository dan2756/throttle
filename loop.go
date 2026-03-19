package main

import (
	"bufio"
	"io"
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

// pump feeds lines from r through t, running t's Tick whenever its
// deadline passes. It returns once the input has ended and every queued
// line has been released.
func pump(t *throttler, r io.Reader) error {
	src := readLines(r)
	defer src.stop()
	in := src.lines

	var (
		timer *time.Timer
		armed time.Time // the deadline timer is set for
	)
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		if in == nil && t.Queued() == 0 {
			return nil
		}

		var wake <-chan time.Time
		if at, ok := t.Deadline(); ok {
			if !at.Equal(armed) {
				d := at.Sub(t.clock.Now())
				if timer == nil {
					timer = time.NewTimer(d)
				} else {
					timer.Reset(d)
				}
				armed = at
			}
			wake = timer.C
		}

		select {
		case line, ok := <-in:
			if !ok {
				if src.err != nil {
					return src.err
				}
				in = nil // keep going until the queue drains
				continue
			}
			if err := t.Line(line); err != nil {
				return err
			}
		case <-wake:
			armed = time.Time{}
			if err := t.Tick(); err != nil {
				return err
			}
		}
	}
}
