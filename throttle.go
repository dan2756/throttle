package main

import (
	"bufio"
	"fmt"
	"io"
	"time"
)

// throttler applies the rate limit to a stream of lines. It does no
// scheduling of its own: the caller feeds it lines with Line, calls Tick
// once the time reported by Deadline has passed, and calls Finish at the
// end. All time comes from clock, so tests can step through it exactly.
type throttler struct {
	cfg    config
	clock  Clock
	bucket *bucket
	out    *bufio.Writer
	notes  io.Writer // where summaries go; nil with --no-summary

	start       time.Time
	windowStart time.Time
	windowDrops int64
	lastDropped []byte // the newest line, if it was dropped (--keep-last only)

	seen    int64
	dropped int64
}

func newThrottler(cfg config, clock Clock, stdout, stderr io.Writer) *throttler {
	now := clock.Now()
	t := &throttler{
		cfg:         cfg,
		clock:       clock,
		bucket:      newBucket(clock, cfg.count, cfg.period, cfg.burst),
		out:         bufio.NewWriter(stdout),
		start:       now,
		windowStart: now,
	}
	switch cfg.summary {
	case summaryStderr:
		t.notes = stderr
	case summaryStdout:
		t.notes = t.out
	}
	return t
}

// Line handles one line of input, including its trailing newline.
func (t *throttler) Line(line []byte) error {
	if err := t.advance(); err != nil {
		return err
	}
	t.seen++
	if t.bucket.Allow() {
		t.lastDropped = nil
		return t.emit(line)
	}
	t.windowDrops++
	t.dropped++
	if t.cfg.keepLast {
		t.lastDropped = line
	}
	return nil
}

// Deadline reports when Tick next needs to run. ok is false when nothing
// is waiting on the clock.
func (t *throttler) Deadline() (at time.Time, ok bool) {
	if t.windowDrops == 0 {
		return time.Time{}, false
	}
	return t.windowStart.Add(t.cfg.period), true
}

// Tick brings the throttler up to the current time, closing the summary
// window if it has ended.
func (t *throttler) Tick() error {
	return t.advance()
}

// Finish emits any line held for --keep-last and reports the totals. Call
// it once, after the last line.
func (t *throttler) Finish() error {
	if line := t.takeLast(); line != nil {
		if err := t.emit(line); err != nil {
			return err
		}
	}
	if t.dropped > 0 {
		elapsed := roundElapsed(t.clock.Now().Sub(t.start))
		t.note("total: dropped %s of %s over %s",
			commas(t.dropped), plural(t.seen, "line"), formatDuration(elapsed))
	}
	return t.out.Flush()
}

func (t *throttler) advance() error {
	elapsed := t.clock.Now().Sub(t.windowStart)
	if elapsed < t.cfg.period {
		return nil
	}
	// Keep windows aligned to the start time even when a tick runs late.
	t.windowStart = t.windowStart.Add(elapsed - elapsed%t.cfg.period)
	return t.closeWindow()
}

// closeWindow reports the drops of the window that just ended. With
// --keep-last it then emits the newest line if that line was dropped, so a
// window never ends with the output showing stale data.
func (t *throttler) closeWindow() error {
	kept := t.takeLast()
	if t.windowDrops > 0 {
		t.note("dropped %s in the last %s", plural(t.windowDrops, "line"), formatDuration(t.cfg.period))
		t.windowDrops = 0
	}
	if kept != nil {
		return t.emit(kept)
	}
	return t.out.Flush()
}

// takeLast returns the line held for --keep-last, if any. Once emitted it
// no longer counts as dropped.
func (t *throttler) takeLast() []byte {
	line := t.lastDropped
	if line != nil {
		t.lastDropped = nil
		t.windowDrops--
		t.dropped--
	}
	return line
}

// emit writes a line and flushes it, keeping the output line-buffered.
func (t *throttler) emit(line []byte) error {
	if _, err := t.out.Write(line); err != nil {
		return err
	}
	return t.out.Flush()
}

// note writes a summary line, unless summaries are off.
func (t *throttler) note(format string, args ...any) {
	if t.notes != nil {
		fmt.Fprintf(t.notes, "[throttle] "+format+"\n", args...)
	}
}
