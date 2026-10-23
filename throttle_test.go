package main

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// harness drives a throttler with a fake clock the way pump does, without
// goroutines or real timers.
type harness struct {
	t     *testing.T
	clock *fakeClock
	th    *throttler
	out   bytes.Buffer
	notes bytes.Buffer
}

func newHarness(t *testing.T, args ...string) *harness {
	t.Helper()
	cfg, err := parseArgs(args)
	if err != nil {
		t.Fatalf("parseArgs(%q): %v", args, err)
	}
	h := &harness{t: t, clock: newFakeClock()}
	h.th = newThrottler(cfg, h.clock, &h.out, &h.notes)
	return h
}

func (h *harness) feed(lines ...string) {
	h.t.Helper()
	for _, l := range lines {
		if err := h.th.Line([]byte(l + "\n")); err != nil {
			h.t.Fatal(err)
		}
	}
}

// advance moves the clock forward by d, stopping at each deadline on the
// way to run Tick, just as the real loop would.
func (h *harness) advance(d time.Duration) {
	h.t.Helper()
	target := h.clock.Now().Add(d)
	for range 100000 {
		at, ok := h.th.Deadline()
		if !ok || at.After(target) {
			h.clock.now = target
			return
		}
		if at.After(h.clock.now) {
			h.clock.now = at
		}
		if err := h.th.Tick(); err != nil {
			h.t.Fatal(err)
		}
	}
	h.t.Fatal("deadline never moved past the target time")
}

func (h *harness) finish() {
	h.t.Helper()
	if err := h.th.Finish(); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) wantOut(want string) {
	h.t.Helper()
	if got := h.out.String(); got != want {
		h.t.Errorf("stdout:\n%s\nwant:\n%s", got, want)
	}
}

func (h *harness) wantNotes(want string) {
	h.t.Helper()
	if got := h.notes.String(); got != want {
		h.t.Errorf("stderr:\n%s\nwant:\n%s", got, want)
	}
}

func lines(s ...string) string {
	if len(s) == 0 {
		return ""
	}
	return strings.Join(s, "\n") + "\n"
}

func TestDropsAreSummarizedPerWindow(t *testing.T) {
	h := newHarness(t, "3")
	h.feed("a", "b", "c", "d", "e")
	h.wantOut(lines("a", "b", "c"))
	h.wantNotes("")

	h.advance(59 * time.Second)
	h.wantNotes("")
	h.advance(time.Second)
	h.wantNotes(lines("[throttle] dropped 2 lines in the last 1m"))

	// Nothing dropped in the next window, so nothing more is said.
	h.advance(time.Minute)
	h.wantNotes(lines("[throttle] dropped 2 lines in the last 1m"))
}

func TestSummaryUsesThePeriodAsWindow(t *testing.T) {
	h := newHarness(t, "2/10s")
	for range 1206 {
		h.feed("x")
	}
	h.advance(10 * time.Second)
	h.wantNotes(lines("[throttle] dropped 1,204 lines in the last 10s"))
}

func TestQuietWindowsAreSilent(t *testing.T) {
	h := newHarness(t, "10")
	h.feed("a", "b")
	if _, ok := h.th.Deadline(); ok {
		t.Error("deadline set although nothing was dropped")
	}
	h.advance(5 * time.Minute)
	h.finish()
	h.wantOut(lines("a", "b"))
	h.wantNotes("")
}

func TestSummaryToStdoutIsInline(t *testing.T) {
	h := newHarness(t, "3", "--summary", "stdout")
	h.feed("a", "b", "c", "d", "e")
	h.advance(time.Minute)
	h.feed("f")
	h.wantOut(lines("a", "b", "c", "[throttle] dropped 2 lines in the last 1m", "f"))
	h.wantNotes("")
}

func TestNoSummary(t *testing.T) {
	h := newHarness(t, "1", "--no-summary")
	h.feed("a", "b", "c")
	h.advance(time.Minute)
	h.finish()
	h.wantOut(lines("a"))
	h.wantNotes("")
}

func TestKeepLastEmitsNewestDroppedLine(t *testing.T) {
	h := newHarness(t, "2", "--keep-last", "--summary", "stdout")
	h.feed("a", "b", "c", "d", "e")
	h.wantOut(lines("a", "b"))
	h.advance(time.Minute)
	// The kept line no longer counts as dropped.
	h.wantOut(lines("a", "b", "[throttle] dropped 2 lines in the last 1m", "e"))
}

func TestKeepLastSkipsLineWhenANewerOnePassed(t *testing.T) {
	h := newHarness(t, "2", "--keep-last") // one token every 30s
	h.feed("a", "b", "c")
	h.advance(30 * time.Second)
	h.feed("d")
	h.advance(30 * time.Second)
	h.wantOut(lines("a", "b", "d"))
	h.wantNotes(lines("[throttle] dropped 1 line in the last 1m"))
}

func TestKeepLastAloneIsNotADrop(t *testing.T) {
	h := newHarness(t, "1", "--keep-last")
	h.feed("a", "b")
	h.advance(time.Minute)
	h.finish()
	h.wantOut(lines("a", "b"))
	h.wantNotes("")
}

func TestWindowsStayAlignedAfterLateTick(t *testing.T) {
	h := newHarness(t, "1")
	start := h.clock.Now()
	h.feed("a")
	h.clock.Advance(30 * time.Second)
	h.feed("b")
	if at, _ := h.th.Deadline(); !at.Equal(start.Add(time.Minute)) {
		t.Fatalf("deadline %v, want start+1m", at.Sub(start))
	}

	// The tick for the first window runs very late.
	h.clock.Advance(100 * time.Second)
	if err := h.th.Tick(); err != nil {
		t.Fatal(err)
	}
	h.wantNotes(lines("[throttle] dropped 1 line in the last 1m"))

	h.feed("c", "d")
	if at, _ := h.th.Deadline(); !at.Equal(start.Add(3 * time.Minute)) {
		t.Errorf("deadline %v, want start+3m", at.Sub(start))
	}
}

func TestUnterminatedLineGetsANewline(t *testing.T) {
	h := newHarness(t, "5", "--summary", "stdout")
	if err := h.th.Line([]byte("no newline")); err != nil {
		t.Fatal(err)
	}
	h.wantOut(lines("no newline"))
}

func TestFinishReportsTotal(t *testing.T) {
	h := newHarness(t, "2")
	for range 10 {
		h.feed("x")
	}
	h.advance(time.Minute)
	for range 5 {
		h.feed("y")
	}
	h.advance(30 * time.Second)
	h.finish()
	h.wantOut(lines("x", "x", "y", "y"))
	h.wantNotes(lines(
		"[throttle] dropped 8 lines in the last 1m",
		"[throttle] total: dropped 11 of 15 lines over 1m30s",
	))
}

func TestFinishEmitsKeptLine(t *testing.T) {
	h := newHarness(t, "1", "--keep-last")
	h.feed("a", "b", "c")
	h.clock.Advance(2 * time.Second)
	h.finish()
	h.wantOut(lines("a", "c"))
	h.wantNotes(lines("[throttle] total: dropped 1 of 3 lines over 2s"))
}

func TestDelayPacesLinesEvenly(t *testing.T) {
	h := newHarness(t, "--delay", "2/s") // one line every 500ms
	h.feed("a", "b", "c", "d")
	h.wantOut(lines("a"))
	if h.th.Queued() != 3 {
		t.Fatalf("queued %d, want 3", h.th.Queued())
	}
	h.advance(499 * time.Millisecond)
	h.wantOut(lines("a"))
	h.advance(time.Millisecond)
	h.wantOut(lines("a", "b"))
	h.advance(time.Second)
	h.wantOut(lines("a", "b", "c", "d"))
	if _, ok := h.th.Deadline(); ok {
		t.Error("deadline still set with an empty queue")
	}

	// After a quiet spell the next line goes straight out.
	h.advance(10 * time.Second)
	h.feed("e")
	h.wantOut(lines("a", "b", "c", "d", "e"))
	h.finish()
	h.wantNotes("")
}

func TestDelayQueuedLinesKeepOrderBehindNewOnes(t *testing.T) {
	h := newHarness(t, "--delay", "1/s")
	h.feed("a", "b")
	h.advance(1500 * time.Millisecond)
	h.feed("c")
	h.advance(500 * time.Millisecond)
	h.wantOut(lines("a", "b", "c"))
}

func TestDelayHonorsExplicitBurst(t *testing.T) {
	h := newHarness(t, "--delay", "1/s", "--burst", "3")
	h.feed("a", "b", "c", "d", "e")
	h.wantOut(lines("a", "b", "c"))
	h.advance(2 * time.Second)
	h.wantOut(lines("a", "b", "c", "d", "e"))
}

func TestDelayBufferDropsOldest(t *testing.T) {
	h := newHarness(t, "--delay", "1/s", "--buffer", "2")
	h.feed("a", "b", "c", "d", "e")
	h.wantOut(lines("a")) // b and c were pushed out by d and e
	h.advance(2 * time.Second)
	h.wantOut(lines("a", "d", "e"))
	h.wantNotes(lines("[throttle] dropped 2 lines in the last 1s"))
	h.finish()
	h.wantNotes(lines(
		"[throttle] dropped 2 lines in the last 1s",
		"[throttle] total: dropped 2 of 5 lines over 2s",
	))
}

func TestDelayUnlimitedBuffer(t *testing.T) {
	h := newHarness(t, "--delay", "1000/s", "--buffer", "0")
	for range 50000 {
		h.feed("x")
	}
	if h.th.Queued() != 49999 {
		t.Fatalf("queued %d, want 49999", h.th.Queued())
	}
	h.advance(50 * time.Second)
	if h.th.Queued() != 0 || h.out.Len() != 100000 {
		t.Errorf("queued %d, wrote %d bytes", h.th.Queued(), h.out.Len())
	}
	h.wantNotes("")
}

func TestFinishCountsQueuedLinesAsDropped(t *testing.T) {
	h := newHarness(t, "--delay", "1/m")
	h.feed("a", "b", "c")
	h.clock.Advance(5 * time.Second)
	h.finish()
	h.wantOut(lines("a"))
	h.wantNotes(lines("[throttle] total: dropped 2 of 3 lines over 5s"))
}

func TestSamplePassesEveryKthOverRateLine(t *testing.T) {
	h := newHarness(t, "2", "--sample", "3")
	for i := 1; i <= 11; i++ {
		h.feed(strconv.Itoa(i))
	}
	h.wantOut(lines("1", "2", "5", "8", "11"))
	h.advance(time.Minute)
	h.wantNotes(lines("[throttle] dropped 6 lines in the last 1m"))
}

func TestSampleCountCarriesAcrossWindows(t *testing.T) {
	h := newHarness(t, "1", "--sample", "2")
	h.feed("a", "b", "c")
	h.advance(time.Minute)
	h.feed("d", "e", "f")
	h.wantOut(lines("a", "c", "d", "f"))
}

type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// linesThenErr accepts n newline-terminated writes, then fails.
type linesThenErr struct {
	n   int
	buf bytes.Buffer
}

func (w *linesThenErr) Write(p []byte) (int, error) {
	if w.n <= 0 {
		return 0, errors.New("disk full")
	}
	w.n -= bytes.Count(p, []byte("\n"))
	return w.buf.Write(p)
}

func TestFailedWriteStaysInTheDropCount(t *testing.T) {
	cfg, err := parseArgs([]string{"5", "--no-summary"})
	if err != nil {
		t.Fatal(err)
	}
	out := &linesThenErr{n: 1}
	th := newThrottler(cfg, newFakeClock(), out, &bytes.Buffer{})
	if err := th.Line([]byte("a\n")); err != nil {
		t.Fatal(err)
	}
	if err := th.Line([]byte("b\n")); err == nil {
		t.Fatal("expected the second line's write to fail")
	}
	if th.seen != 2 || th.dropped != 1 || out.buf.String() != "a\n" {
		t.Fatalf("seen %d dropped %d out %q", th.seen, th.dropped, out.buf.String())
	}
}

func TestFailedDelayWriteStaysInTheDropCount(t *testing.T) {
	cfg, err := parseArgs([]string{"--delay", "1/s", "--buffer", "1", "--no-summary"})
	if err != nil {
		t.Fatal(err)
	}
	clock := newFakeClock()
	out := &linesThenErr{n: 1}
	th := newThrottler(cfg, clock, out, &bytes.Buffer{})
	for _, l := range []string{"a", "b", "c"} {
		if err := th.Line([]byte(l + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	clock.Advance(time.Second)
	if err := th.Tick(); err == nil {
		t.Fatal("expected the queued line's write to fail")
	}
	// a was written, b was dropped to make room, c was popped and failed to write.
	if th.seen != 3 || th.dropped != 2 || out.buf.String() != "a\n" {
		t.Fatalf("seen %d dropped %d out %q", th.seen, th.dropped, out.buf.String())
	}
}

func TestFailedKeepLastWriteStaysInTheDropCount(t *testing.T) {
	cfg, err := parseArgs([]string{"1", "--keep-last", "--no-summary"})
	if err != nil {
		t.Fatal(err)
	}
	clock := newFakeClock()
	out := &linesThenErr{n: 1}
	var notes bytes.Buffer
	th := newThrottler(cfg, clock, out, &notes)
	if err := th.Line([]byte("a\n")); err != nil {
		t.Fatal(err)
	}
	if err := th.Line([]byte("b\n")); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	if err := th.Finish(); err == nil {
		t.Fatal("expected the kept line's write to fail")
	}
	if th.seen != 2 || th.dropped != 1 || out.buf.String() != "a\n" || notes.Len() != 0 {
		t.Fatalf("seen %d dropped %d out %q notes %q", th.seen, th.dropped, out.buf.String(), notes.String())
	}
}

func TestWindowSummaryWriteError(t *testing.T) {
	cfg, err := parseArgs([]string{"1"})
	if err != nil {
		t.Fatal(err)
	}
	clock := newFakeClock()
	var out bytes.Buffer
	th := newThrottler(cfg, clock, &out, errWriter{errors.New("closed")})
	if err := th.Line([]byte("a\n")); err != nil {
		t.Fatal(err)
	}
	if err := th.Line([]byte("b\n")); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	err = th.Tick()
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Tick err = %v, want the summary write error", err)
	}
	if out.String() != "a\n" {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestSampleWithKeepLast(t *testing.T) {
	h := newHarness(t, "1", "--sample", "2", "--keep-last")
	h.feed("a", "b", "c", "d")
	h.advance(time.Minute)
	// b is simply dropped; c is sampled; d is dropped, then kept.
	h.wantOut(lines("a", "c", "d"))
	h.wantNotes(lines("[throttle] dropped 1 line in the last 1m"))
}
