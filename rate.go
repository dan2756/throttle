package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// parseRate parses COUNT or COUNT/PERIOD, e.g. "30", "5/s" or "100/10m".
// period is zero when s does not name one.
func parseRate(s string) (count int64, period time.Duration, err error) {
	c, p, hasPeriod := strings.Cut(s, "/")
	count, err = strconv.ParseInt(c, 10, 64)
	if err != nil || count < 1 {
		return 0, 0, fmt.Errorf("invalid rate %q: count must be a positive integer", s)
	}
	if hasPeriod {
		period, err = parsePeriod(p)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid rate %q: %v", s, err)
		}
	}
	return count, period, nil
}

var periodUnits = map[string]time.Duration{
	"ms":      time.Millisecond,
	"s":       time.Second,
	"sec":     time.Second,
	"secs":    time.Second,
	"second":  time.Second,
	"seconds": time.Second,
	"m":       time.Minute,
	"min":     time.Minute,
	"mins":    time.Minute,
	"minute":  time.Minute,
	"minutes": time.Minute,
	"h":       time.Hour,
	"hr":      time.Hour,
	"hrs":     time.Hour,
	"hour":    time.Hour,
	"hours":   time.Hour,
	"d":       24 * time.Hour,
	"day":     24 * time.Hour,
	"days":    24 * time.Hour,
}

// parsePeriod parses a period such as "s", "10m", "1.5s", "30sec" or
// "1h30m". A bare unit means one of it.
func parsePeriod(s string) (time.Duration, error) {
	num, unit := s, ""
	if i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' }); i >= 0 {
		num, unit = s[:i], s[i:]
	}
	if u, ok := periodUnits[strings.ToLower(unit)]; ok {
		n := 1.0
		if num != "" {
			var err error
			if n, err = strconv.ParseFloat(num, 64); err != nil {
				return 0, fmt.Errorf("invalid period %q", s)
			}
		}
		f := n * float64(u)
		if f <= 0 {
			return 0, fmt.Errorf("period %q must be positive", s)
		}
		// MaxInt64 rounds up to 2^63 as a float64, which is already too big.
		if f < 1 || f >= math.MaxInt64 {
			return 0, fmt.Errorf("period %q out of range", s)
		}
		return time.Duration(f), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid period %q", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("period %q must be positive", s)
	}
	return d, nil
}
