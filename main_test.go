package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
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
		{[]string{"30", "--no-summary", "--keep-last"}, 30, time.Minute, 30},
		{[]string{"--delay", "10/s"}, 10, time.Second, 1},
		{[]string{"5/s", "--sample", "10", "--keep-last"}, 5, time.Second, 5},
		{[]string{"--delay", "10/s", "--burst", "4", "--buffer", "0"}, 10, time.Second, 4},
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
		{[]string{"30", "--buffer", "50"}, "--buffer only applies with --delay"},
		{[]string{"30", "--delay", "--buffer", "-1"}, "--buffer must be 0 or more"},
		{[]string{"30", "--delay", "--keep-last"}, "--keep-last can't be used with --delay"},
		{[]string{"30", "--sample", "1"}, "--sample must be at least 2"},
		{[]string{"30", "--sample", "0"}, "--sample must be at least 2"},
		{[]string{"30", "--delay", "--sample", "4"}, "--sample can't be used with --delay"},
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

// runThrottle runs the command end to end on the given input.
func runThrottle(t *testing.T, stdin io.Reader, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, stdin, &out, &errOut, nil)
	return out.String(), errOut.String(), code
}

func numbered(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%d\n", i)
	}
	return b.String()
}

var totalRE = regexp.MustCompile(`^\[throttle\] total: dropped (\S+) of (\S+) lines over \S+\n$`)

func TestEndToEnd(t *testing.T) {
	out, errOut, code := runThrottle(t, strings.NewReader(numbered(10)), "3")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if out != "1\n2\n3\n" {
		t.Errorf("stdout %q", out)
	}
	m := totalRE.FindStringSubmatch(errOut)
	if m == nil || m[1] != "7" || m[2] != "10" {
		t.Errorf("stderr %q, want a total of 7 of 10 dropped", errOut)
	}
}

func TestEndToEndKeepLastWithInlineSummary(t *testing.T) {
	out, errOut, code := runThrottle(t, strings.NewReader(numbered(10)), "--keep-last", "3/m", "--summary", "stdout")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	body, summary, _ := strings.Cut(out, "[throttle]")
	if body != "1\n2\n3\n10\n" {
		t.Errorf("stdout lines %q", body)
	}
	if m := totalRE.FindStringSubmatch("[throttle]" + summary); m == nil || m[1] != "6" {
		t.Errorf("summary %q, want 6 of 10 dropped", summary)
	}
}

func TestEndToEndUnderRateIsSilent(t *testing.T) {
	in := numbered(5)
	out, errOut, code := runThrottle(t, strings.NewReader(in), "100/s")
	if code != 0 || out != in || errOut != "" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestEndToEndLongLines(t *testing.T) {
	long1 := strings.Repeat("a", 200_000)
	long2 := strings.Repeat("b", 1<<20)
	in := long1 + "\n" + long2 + "\nshort\n"
	out, errOut, code := runThrottle(t, strings.NewReader(in), "2", "--no-summary")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if out != long1+"\n"+long2+"\n" {
		t.Errorf("long lines not passed intact (got %d bytes)", len(out))
	}
}

func TestEndToEndSample(t *testing.T) {
	out, errOut, code := runThrottle(t, strings.NewReader(numbered(100)), "10", "--sample", "10")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	// 1-10 fit the burst; of the 90 lines over the rate, every 10th passes.
	want := numbered(10) + "20\n30\n40\n50\n60\n70\n80\n90\n100\n"
	if out != want {
		t.Errorf("stdout %q, want %q", out, want)
	}
	if m := totalRE.FindStringSubmatch(errOut); m == nil || m[1] != "81" {
		t.Errorf("stderr %q, want 81 dropped", errOut)
	}
}

func TestEndToEndDelay(t *testing.T) {
	in := numbered(5)
	start := time.Now()
	out, errOut, code := runThrottle(t, strings.NewReader(in), "--delay", "200/s")
	elapsed := time.Since(start)
	if code != 0 || out != in || errOut != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	// Five lines paced 5ms apart can't all be out in under 20ms.
	if elapsed < 20*time.Millisecond {
		t.Errorf("finished in %v; lines were not paced", elapsed)
	}
}

// interruptingReader returns its data on the first Read. On the next one,
// by which time every line of the data has been handed to the main loop,
// it sends sig and then blocks until the test ends, like a terminal that
// someone pressed Ctrl-C in.
type interruptingReader struct {
	data string
	sig  os.Signal
	stop chan os.Signal
	done chan struct{}
	read bool
}

func newInterruptingReader(t *testing.T, data string, sig os.Signal) *interruptingReader {
	r := &interruptingReader{data: data, sig: sig, stop: make(chan os.Signal, 1), done: make(chan struct{})}
	t.Cleanup(func() { close(r.done) })
	return r
}

func (r *interruptingReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(p, r.data), nil
	}
	r.stop <- r.sig
	<-r.done
	return 0, io.EOF
}

func TestInterruptPrintsTotal(t *testing.T) {
	r := newInterruptingReader(t, numbered(10), syscall.SIGINT)
	var out, errOut bytes.Buffer
	code := run([]string{"3"}, r, &out, &errOut, r.stop)
	if code != 130 {
		t.Errorf("exit %d, want 130", code)
	}
	if out.String() != "1\n2\n3\n" {
		t.Errorf("stdout %q", out.String())
	}
	if m := totalRE.FindStringSubmatch(errOut.String()); m == nil || m[1] != "7" || m[2] != "10" {
		t.Errorf("stderr %q, want 7 of 10 dropped", errOut.String())
	}
}

func TestTerminateDiscardsQueuedLines(t *testing.T) {
	r := newInterruptingReader(t, numbered(4), syscall.SIGTERM)
	var out, errOut bytes.Buffer
	code := run([]string{"--delay", "1/h"}, r, &out, &errOut, r.stop)
	if code != 143 {
		t.Errorf("exit %d, want 143", code)
	}
	if out.String() != "1\n" {
		t.Errorf("stdout %q", out.String())
	}
	if m := totalRE.FindStringSubmatch(errOut.String()); m == nil || m[1] != "3" || m[2] != "4" {
		t.Errorf("stderr %q, want 3 of 4 dropped", errOut.String())
	}
}

func TestEndToEndReadError(t *testing.T) {
	_, errOut, code := runThrottle(t, iotest.ErrReader(errors.New("boom")), "3")
	if code != 1 || errOut != "throttle: boom\n" {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestRunUsage(t *testing.T) {
	out, _, code := runThrottle(t, strings.NewReader(""), "-h")
	if code != 0 || !strings.Contains(out, "Usage: throttle") {
		t.Errorf("-h: exit %d, stdout %q", code, out)
	}
	out, _, code = runThrottle(t, strings.NewReader(""), "--version")
	if code != 0 || out != "throttle "+version+"\n" {
		t.Errorf("--version: exit %d, stdout %q", code, out)
	}
	_, errOut, code := runThrottle(t, strings.NewReader(""), "--summary", "syslog", "5")
	if code != 2 || !strings.Contains(errOut, "--summary must be stderr or stdout") ||
		!strings.Contains(errOut, "throttle --help") {
		t.Errorf("bad flag: exit %d, stderr %q", code, errOut)
	}
}
