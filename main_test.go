package main

import (
	"errors"
	"flag"
	"strings"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		args   []string
		count  int64
		period time.Duration
		burst  int64
	}{
		{[]string{"30"}, 30, time.Minute, 30},
		{[]string{"5/s"}, 5, time.Second, 5},
		{[]string{"100/10m"}, 100, 10 * time.Minute, 100},
		{[]string{"100", "--per", "10s"}, 100, 10 * time.Second, 100},
		{[]string{"--per=s", "20"}, 20, time.Second, 20},
		{[]string{"30", "--burst", "5"}, 30, time.Minute, 5},
		{[]string{"-burst=100", "30"}, 30, time.Minute, 100},
	}
	for _, tt := range tests {
		cfg, err := parseArgs(tt.args)
		if err != nil {
			t.Errorf("parseArgs(%q): %v", tt.args, err)
			continue
		}
		if cfg.count != tt.count || cfg.period != tt.period || cfg.burst != tt.burst {
			t.Errorf("parseArgs(%q) = count %d, period %v, burst %d; want %d, %v, %d",
				tt.args, cfg.count, cfg.period, cfg.burst, tt.count, tt.period, tt.burst)
		}
	}
}

func TestParseArgsErrors(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{nil, "missing RATE"},
		{[]string{"30", "40"}, `unexpected argument "40"`},
		{[]string{"5/s", "--per", "10s"}, "--per conflicts"},
		{[]string{"5", "--per", "soon"}, "--per: invalid period"},
		{[]string{"30", "--burst", "0"}, "--burst must be at least 1"},
		{[]string{"30", "--burst", "lots"}, "invalid value"},
		{[]string{"--frobnicate", "30"}, "flag provided but not defined"},
		{[]string{"2000000000/s"}, "too fast"},
	}
	for _, tt := range tests {
		_, err := parseArgs(tt.args)
		if err == nil {
			t.Errorf("parseArgs(%q): expected an error", tt.args)
			continue
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Errorf("parseArgs(%q) error %q, want it to mention %q", tt.args, err, tt.want)
		}
	}
}

func TestParseArgsHelpAndVersion(t *testing.T) {
	if _, err := parseArgs([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("--help: got %v, want flag.ErrHelp", err)
	}
	cfg, err := parseArgs([]string{"--version"})
	if err != nil || !cfg.showVersion {
		t.Errorf("--version: got %+v, %v", cfg, err)
	}
}
