package main

import (
	"testing"
	"time"
)

// take reports how many of n calls to Allow succeed.
func take(b *bucket, n int) int {
	ok := 0
	for range n {
		if b.Allow() {
			ok++
		}
	}
	return ok
}

func TestBucketStartsFull(t *testing.T) {
	clock := newFakeClock()
	b := newBucket(clock, 30, time.Minute, 30)
	if got := take(b, 100); got != 30 {
		t.Fatalf("initial burst: got %d, want 30", got)
	}
}

func TestBucketRefillsOneTokenPerInterval(t *testing.T) {
	clock := newFakeClock()
	b := newBucket(clock, 30, time.Minute, 3) // one token every 2s
	if got := take(b, 10); got != 3 {
		t.Fatalf("burst: got %d, want 3", got)
	}
	clock.Advance(1999 * time.Millisecond)
	if b.Allow() {
		t.Fatal("token available before a full interval passed")
	}
	clock.Advance(time.Millisecond)
	if !b.Allow() {
		t.Fatal("no token after a full interval")
	}
	if b.Allow() {
		t.Fatal("second token after a single interval")
	}
}

func TestBucketAccruesAcrossCalls(t *testing.T) {
	clock := newFakeClock()
	b := newBucket(clock, 1, time.Second, 1)
	b.Allow()
	for i := range 3 {
		clock.Advance(250 * time.Millisecond)
		if b.Allow() {
			t.Fatalf("token after only %dms", (i+1)*250)
		}
	}
	clock.Advance(250 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("partial progress was lost between calls")
	}
}

func TestBucketCapsAtBurst(t *testing.T) {
	clock := newFakeClock()
	b := newBucket(clock, 5, time.Second, 5)
	take(b, 5)
	clock.Advance(time.Hour)
	if got := take(b, 100); got != 5 {
		t.Fatalf("after long idle: got %d, want burst of 5", got)
	}
}

func TestBucketFullBucketDoesNotBankTime(t *testing.T) {
	clock := newFakeClock()
	b := newBucket(clock, 1, time.Second, 1)
	clock.Advance(900 * time.Millisecond) // full, so this earns nothing
	if !b.Allow() {
		t.Fatal("full bucket refused")
	}
	clock.Advance(200 * time.Millisecond)
	if b.Allow() {
		t.Fatal("time spent full was banked toward the next token")
	}
	clock.Advance(800 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("no token one interval after spending")
	}
}

func TestBucketUnevenInterval(t *testing.T) {
	clock := newFakeClock()
	b := newBucket(clock, 3, time.Second, 10) // 333.333333ms per token
	take(b, 10)
	passed := 0
	for range 3000 {
		clock.Advance(time.Millisecond)
		if b.Allow() {
			passed++
		}
	}
	if passed != 9 {
		t.Fatalf("3/s over 3s: got %d, want 9", passed)
	}
}

func TestBucketWait(t *testing.T) {
	clock := newFakeClock()
	b := newBucket(clock, 2, time.Second, 1) // one token every 500ms
	if w := b.Wait(); w != 0 {
		t.Fatalf("full bucket: Wait() = %v, want 0", w)
	}
	b.Allow()
	if w := b.Wait(); w != 500*time.Millisecond {
		t.Fatalf("empty bucket: Wait() = %v, want 500ms", w)
	}
	clock.Advance(200 * time.Millisecond)
	if w := b.Wait(); w != 300*time.Millisecond {
		t.Fatalf("after 200ms: Wait() = %v, want 300ms", w)
	}
	clock.Advance(300 * time.Millisecond)
	if w := b.Wait(); w != 0 {
		t.Fatalf("after a full interval: Wait() = %v, want 0", w)
	}
	if b.tokens != 1 {
		t.Fatalf("Wait spent or lost a token: tokens = %d", b.tokens)
	}
}
