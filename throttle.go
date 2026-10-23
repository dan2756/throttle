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
	overRate    int64  // lines that found the bucket empty (for --sample)
	queue       queue  // lines waiting to be released (--delay only)

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
	if t.cfg.delay {
		return t.enqueue(line)
	}
	if t.bucket.Allow() {
		t.lastDropped = nil
		return t.emit(line)
	}
	// Over the rate. With --sample every Kth such line still gets through;
	// the count runs across windows so a steady overload samples steadily.
	t.overRate++
	if t.cfg.sample > 0 && t.overRate%t.cfg.sample == 0 {
		t.lastDropped = nil
		return t.emit(line)
	}
	t.drop()
	if t.cfg.keepLast {
		t.lastDropped = line
	}
	return nil
}

// enqueue adds a line to the --delay queue, making room by dropping the
// oldest queued line if the buffer is full, then releases what it can.
func (t *throttler) enqueue(line []byte) error {
	t.queue.push(line)
	if t.cfg.buffer > 0 && t.queue.len() > t.cfg.buffer {
		t.queue.pop()
		t.drop()
	}
	return t.release()
}

// release emits queued lines for as long as the bucket has tokens.
func (t *throttler) release() error {
	for t.queue.len() > 0 && t.bucket.Allow() {
		if err := t.emit(t.queue.pop()); err != nil {
			return err
		}
	}
	return nil
}

// Queued reports how many lines are waiting to be released.
func (t *throttler) Queued() int {
	return t.queue.len()
}

// Deadline reports when Tick next needs to run: the end of the current
// window if it has drops to report, or the time the next queued line may
// go out, whichever is sooner. ok is false when nothing is waiting on the
// clock.
func (t *throttler) Deadline() (at time.Time, ok bool) {
	if t.windowDrops > 0 {
		at, ok = t.windowStart.Add(t.cfg.period), true
	}
	if t.queue.len() > 0 {
		next := t.clock.Now().Add(t.bucket.Wait())
		if !ok || next.Before(at) {
			at, ok = next, true
		}
	}
	return at, ok
}

// Tick brings the throttler up to the current time: it closes the summary
// window if it has ended and releases queued lines that are due.
func (t *throttler) Tick() error {
	return t.advance()
}

// Finish emits any line held for --keep-last and reports the totals. Call
// it once, after the last line. Lines still queued for --delay at this
// point are discarded and counted as dropped.
func (t *throttler) Finish() error {
	if n := t.queue.len(); n > 0 {
		t.dropped += int64(n)
		t.queue = queue{}
	}
	if line := t.takeLast(); line != nil {
		if err := t.emit(line); err != nil {
			return err
		}
	}
	if t.dropped > 0 {
		elapsed := roundElapsed(t.clock.Now().Sub(t.start))
		if err := t.note("total: dropped %s of %s over %s",
			commas(t.dropped), plural(t.seen, "line"), formatDuration(elapsed)); err != nil {
			t.out.Flush()
			return err
		}
	}
	return t.out.Flush()
}

func (t *throttler) advance() error {
	if elapsed := t.clock.Now().Sub(t.windowStart); elapsed >= t.cfg.period {
		// Keep windows aligned to the start time even when a tick runs late.
		t.windowStart = t.windowStart.Add(elapsed - elapsed%t.cfg.period)
		if err := t.closeWindow(); err != nil {
			return err
		}
	}
	return t.release()
}

// closeWindow reports the drops of the window that just ended. With
// --keep-last it then emits the newest line if that line was dropped, so a
// window never ends with the output showing stale data.
func (t *throttler) closeWindow() error {
	kept := t.takeLast()
	var nerr error
	if t.windowDrops > 0 {
		nerr = t.note("dropped %s in the last %s", plural(t.windowDrops, "line"), formatDuration(t.cfg.period))
		if nerr == nil {
			t.windowDrops = 0
		}
	}
	if kept != nil {
		if err := t.emit(kept); err != nil {
			return err
		}
	} else if err := t.out.Flush(); err != nil {
		return err
	}
	return nerr
}

func (t *throttler) drop() {
	t.windowDrops++
	t.dropped++
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

// emit writes a line and flushes it, keeping the output line-buffered. A
// last line that arrived without a newline gets one, so anything written
// after it (a summary, a kept line) starts on a line of its own.
func (t *throttler) emit(line []byte) error {
	if _, err := t.out.Write(line); err != nil {
		return err
	}
	if len(line) == 0 || line[len(line)-1] != '\n' {
		if err := t.out.WriteByte('\n'); err != nil {
			return err
		}
	}
	return t.out.Flush()
}

// note writes a summary line, unless summaries are off.
func (t *throttler) note(format string, args ...any) error {
	if t.notes == nil {
		return nil
	}
	_, err := fmt.Fprintf(t.notes, "[throttle] "+format+"\n", args...)
	return err
}
