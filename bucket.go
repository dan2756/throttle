package main

import "time"

// bucket is a token bucket holding up to burst tokens. A token is added
// every interval and each line that passes spends one. All arithmetic is in
// whole nanoseconds, so results are exact and reproducible.
type bucket struct {
	clock    Clock
	interval time.Duration // time to earn one token
	burst    int64
	tokens   int64
	accrued  time.Duration // progress toward the next token, always < interval
	last     time.Time
}

// newBucket returns a full bucket that earns count tokens per period.
func newBucket(clock Clock, count int64, period time.Duration, burst int64) *bucket {
	return &bucket{
		clock:    clock,
		interval: period / time.Duration(count),
		burst:    burst,
		tokens:   burst,
		last:     clock.Now(),
	}
}

func (b *bucket) refill() {
	now := b.clock.Now()
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return
	}
	b.last = now
	if b.tokens >= b.burst {
		// A full bucket earns nothing; accrual restarts once a token is spent.
		b.accrued = 0
		return
	}
	total := b.accrued + elapsed
	n := int64(total / b.interval)
	if n >= b.burst-b.tokens {
		b.tokens, b.accrued = b.burst, 0
		return
	}
	b.tokens += n
	b.accrued = total % b.interval
}

// Allow spends a token and reports true if one is available.
func (b *bucket) Allow() bool {
	b.refill()
	if b.tokens == 0 {
		return false
	}
	b.tokens--
	return true
}
