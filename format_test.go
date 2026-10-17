package main

import (
	"testing"
	"time"
)

func TestCommas(t *testing.T) {
	tests := map[int64]string{
		0:          "0",
		7:          "7",
		999:        "999",
		1000:       "1,000",
		1204:       "1,204",
		999999:     "999,999",
		1234567:    "1,234,567",
		-1234567:   "-1,234,567",
		-999:       "-999",
		1000000000: "1,000,000,000",
	}
	for n, want := range tests {
		if got := commas(n); got != want {
			t.Errorf("commas(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int64]string{0: "0 lines", 1: "1 line", 2: "2 lines", 1204: "1,204 lines"} {
		if got := plural(n, "line"); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[time.Duration]string{
		0:                                  "0s",
		500 * time.Millisecond:             "500ms",
		1500 * time.Millisecond:            "1.5s",
		time.Second:                        "1s",
		time.Minute:                        "1m",
		10 * time.Minute:                   "10m",
		90 * time.Second:                   "1m30s",
		time.Hour:                          "1h",
		90 * time.Minute:                   "1h30m",
		time.Hour + time.Second:            "1h0m1s",
		24 * time.Hour:                     "24h",
		12*time.Minute + 4*time.Second:     "12m4s",
		time.Minute + 500*time.Millisecond: "1m0.5s",
	}
	for d, want := range tests {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestRoundElapsed(t *testing.T) {
	tests := map[time.Duration]time.Duration{
		312345 * time.Nanosecond:                              312 * time.Microsecond,
		1234567 * time.Nanosecond:                             1 * time.Millisecond,
		4249 * time.Millisecond:                               4200 * time.Millisecond,
		59960 * time.Millisecond:                              time.Minute,
		12*time.Minute + 4*time.Second + 400*time.Millisecond: 12*time.Minute + 4*time.Second,
	}
	for d, want := range tests {
		if got := roundElapsed(d); got != want {
			t.Errorf("roundElapsed(%v) = %v, want %v", d, got, want)
		}
	}
}
