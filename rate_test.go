package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseRate(t *testing.T) {
	tests := []struct {
		in     string
		count  int64
		period time.Duration
	}{
		{"30", 30, 0},
		{"1", 1, 0},
		{"5/s", 5, time.Second},
		{"5/sec", 5, time.Second},
		{"120/m", 120, time.Minute},
		{"120/min", 120, time.Minute},
		{"100/10m", 100, 10 * time.Minute},
		{"100/10min", 100, 10 * time.Minute},
		{"3/1.5s", 3, 1500 * time.Millisecond},
		{"10/500ms", 10, 500 * time.Millisecond},
		{"1000/h", 1000, time.Hour},
		{"1000/Hour", 1000, time.Hour},
		{"50/1h30m", 50, 90 * time.Minute},
		{"7/d", 7, 24 * time.Hour},
	}
	for _, tt := range tests {
		count, period, err := parseRate(tt.in)
		if err != nil {
			t.Errorf("parseRate(%q): %v", tt.in, err)
			continue
		}
		if count != tt.count || period != tt.period {
			t.Errorf("parseRate(%q) = %d, %v; want %d, %v", tt.in, count, period, tt.count, tt.period)
		}
	}
}

func TestParseRateErrors(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "count must be a positive integer"},
		{"0", "count must be a positive integer"},
		{"-5", "count must be a positive integer"},
		{"2.5", "count must be a positive integer"},
		{"abc", "count must be a positive integer"},
		{"/s", "count must be a positive integer"},
		{"5/", "invalid period"},
		{"5/fortnight", "invalid period"},
		{"5/0s", "must be positive"},
		{"5/0", "must be positive"},
		{"5/-1m", "must be positive"},
		{"5/1..5s", "invalid period"},
		{"5/106752d", "out of range"},
		{"5/106751.99116730064d", "out of range"},   // exactly 2^63ns as a float64
		{"5/9223372036.854775807s", "out of range"}, // likewise
	}
	for _, tt := range tests {
		_, _, err := parseRate(tt.in)
		if err == nil {
			t.Errorf("parseRate(%q): expected an error", tt.in)
			continue
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Errorf("parseRate(%q) error %q, want it to mention %q", tt.in, err, tt.want)
		}
	}
}
