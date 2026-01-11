package main

import (
	"bufio"
	"fmt"
	"io"
)

// throttler copies lines from an input to an output, dropping any that
// arrive faster than its bucket allows.
type throttler struct {
	bucket *bucket
	out    *bufio.Writer
	notes  io.Writer

	seen    int64
	dropped int64
}

func newThrottler(cfg config, clock Clock, stdout, stderr io.Writer) *throttler {
	return &throttler{
		bucket: newBucket(clock, cfg.count, cfg.period, cfg.burst),
		out:    bufio.NewWriter(stdout),
		notes:  stderr,
	}
}

// Run reads r line by line until EOF. Lines that pass are written and
// flushed immediately so the output stays line-buffered.
func (t *throttler) Run(r io.Reader) error {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		t.seen++
		if !t.bucket.Allow() {
			t.dropped++
			continue
		}
		if _, err := t.out.Write(line); err != nil {
			return err
		}
		if err := t.out.Flush(); err != nil {
			return err
		}
	}
	if t.dropped > 0 {
		fmt.Fprintf(t.notes, "[throttle] dropped %d of %d lines\n", t.dropped, t.seen)
	}
	return t.out.Flush()
}
