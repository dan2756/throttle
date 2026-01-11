// Command throttle limits a stream of lines to a fixed rate.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

const version = "0.2.0"

const usage = `Usage: throttle [flags] RATE

Copy lines from stdin to stdout, letting at most RATE through and dropping
the rest.

RATE is COUNT or COUNT/PERIOD:
  30          30 lines per minute
  5/s         5 lines per second
  100/10m     100 lines every 10 minutes

Flags:
  --per PERIOD    period for a bare COUNT (default 1m)
  --burst N       lines that may pass at once after a quiet spell
                  (default COUNT)
  -h, --help      show this help
  --version       print the version and exit

Examples:
  tail -f app.log | throttle 30
  journalctl -f | throttle 5/s
  ./noisy-job 2>&1 | throttle 100 --per 10s --burst 20
`

// config is the parsed command line.
type config struct {
	count  int64
	period time.Duration
	burst  int64

	showVersion bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := parseArgs(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(stdout, usage)
		return 0
	case err != nil:
		fmt.Fprintf(stderr, "throttle: %v\nTry 'throttle --help' for more information.\n", err)
		return 2
	case cfg.showVersion:
		fmt.Fprintf(stdout, "throttle %s\n", version)
		return 0
	}

	t := newThrottler(cfg, realClock{}, stdout, stderr)
	if err := t.Run(stdin); err != nil {
		fmt.Fprintf(stderr, "throttle: %v\n", err)
		return 1
	}
	return 0
}

// parseArgs parses the command line. Flags may appear before or after RATE.
func parseArgs(args []string) (config, error) {
	var (
		cfg   config
		per   string
		burst int64
	)
	fs := flag.NewFlagSet("throttle", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&per, "per", "", "")
	fs.Int64Var(&burst, "burst", 0, "")
	fs.BoolVar(&cfg.showVersion, "version", false, "")

	// The flag package stops at the first non-flag argument, so peel off
	// positionals one at a time and keep parsing what follows them.
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return cfg, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
	if cfg.showVersion {
		return cfg, nil
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	switch len(positional) {
	case 0:
		return cfg, errors.New("missing RATE")
	case 1:
	default:
		return cfg, fmt.Errorf("unexpected argument %q", positional[1])
	}
	count, period, err := parseRate(positional[0])
	if err != nil {
		return cfg, err
	}
	if set["per"] {
		if period != 0 {
			return cfg, fmt.Errorf("--per conflicts with the period in %q", positional[0])
		}
		if period, err = parsePeriod(per); err != nil {
			return cfg, fmt.Errorf("--per: %v", err)
		}
	}
	if period == 0 {
		period = time.Minute
	}
	if period/time.Duration(count) == 0 {
		return cfg, fmt.Errorf("rate %q is too fast", positional[0])
	}
	cfg.count, cfg.period = count, period

	cfg.burst = count
	if set["burst"] {
		if burst < 1 {
			return cfg, errors.New("--burst must be at least 1")
		}
		cfg.burst = burst
	}
	return cfg, nil
}
