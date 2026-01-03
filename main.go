// Command throttle limits a stream of lines to at most N per minute.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

const version = "0.1.0"

const usage = `Usage: throttle [flags] N

Copy lines from stdin to stdout, letting at most N lines through per minute
and dropping the rest. A quiet stream may burst up to N lines at once.

Flags:
  -h, --help    show this help
  --version     print the version and exit

Example:
  tail -f app.log | throttle 30
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("throttle", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	showVersion := fs.Bool("version", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		return usageError(stderr, err)
	}
	if *showVersion {
		fmt.Fprintf(stdout, "throttle %s\n", version)
		return 0
	}
	if fs.NArg() != 1 {
		return usageError(stderr, errors.New("expected exactly one argument, the number of lines per minute"))
	}
	n, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil || n < 1 {
		return usageError(stderr, fmt.Errorf("invalid rate %q: must be a positive integer", fs.Arg(0)))
	}

	t := newThrottler(realClock{}, n, time.Minute, stdout, stderr)
	if err := t.Run(stdin); err != nil {
		fmt.Fprintf(stderr, "throttle: %v\n", err)
		return 1
	}
	return 0
}

func usageError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "throttle: %v\nTry 'throttle --help' for more information.\n", err)
	return 2
}
