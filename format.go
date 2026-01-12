package main

import (
	"strconv"
	"strings"
	"time"
)

// commas formats n with thousands separators: 1204 -> "1,204".
func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	start := 0
	if n < 0 {
		start = 1
	}
	for i := len(s) - 3; i > start; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// plural formats a count with a noun: "1 line", "1,204 lines".
func plural(n int64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return commas(n) + " " + noun + "s"
}

// formatDuration renders d compactly, without trailing zero units:
// "1m" rather than "1m0s", "2h" rather than "2h0m0s".
func formatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

// roundElapsed rounds a run time to a precision that reads well in a
// summary: milliseconds under a second, tenths under a minute, else seconds.
func roundElapsed(d time.Duration) time.Duration {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond)
	case d < time.Minute:
		return d.Round(100 * time.Millisecond)
	default:
		return d.Round(time.Second)
	}
}
