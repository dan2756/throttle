package main

import "time"

// Clock is the source of time for the limiter. Tests substitute a fake so
// that rate behavior can be checked without sleeping.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
