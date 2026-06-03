# throttle

Limits output to N lines per minute.

Pipe a noisy stream through `throttle` and at most N lines per minute come
out the other side; the rest are dropped, and you're told how many. Or, with
`--delay`, nothing is dropped and lines are paced out evenly instead. Handy
for chatty logs, runaway debug output, replaying events into something slow,
or anything you want to watch without drowning in it.

## Install

```
go install github.com/dan2756/throttle@latest
```

## Usage

```
throttle [flags] RATE
```

`RATE` is a count, optionally followed by `/PERIOD`:

| Rate               | Meaning                      |
| ------------------ | ---------------------------- |
| `30`               | 30 lines per minute          |
| `5/s`              | 5 lines per second           |
| `100/10m`          | 100 lines every 10 minutes   |
| `1000/h`           | 1000 lines per hour          |
| `100 --per 10s`    | 100 lines every 10 seconds   |

Periods accept a bare unit (`s`, `m`, `h`, `d`, `ms`, or spelled out like
`sec`, `min`, `hour`), a number with a unit (`10m`, `1.5s`), or any Go
duration (`1h30m`).

| Flag             | Description                                                     |
| ---------------- | --------------------------------------------------------------- |
| `--per PERIOD`   | Period for a bare count (default `1m`).                         |
| `--burst N`      | Lines that may pass at once after a quiet spell (default: the count, or 1 with `--delay`). |
| `--summary DEST` | Where summaries go: `stderr` (default) or `stdout` (inline with the lines). |
| `--no-summary`   | Don't report dropped lines at all.                              |
| `--keep-last`    | At the end of each period, emit the newest line if it was dropped. |
| `--sample K`     | When over the rate, let every Kth line through instead of dropping it. |
| `--delay`        | Don't drop; queue lines and release them evenly at the rate.    |
| `--buffer N`     | With `--delay`, the most lines to queue (default 10000, `0` = no limit). |
| `-h`, `--help`   | Show help.                                                      |
| `--version`      | Print the version.                                              |

Flags can go before or after the rate.

### How the limit works

`throttle` uses a token bucket. The bucket holds `--burst` tokens and starts
full; each line that passes spends one, and tokens come back at the rate
(for `30`, one every 2 seconds). By default the burst equals the count, so a
stream that has been quiet can let a full period's worth of lines through at
once, then settles to the steady rate.

### Summaries

Dropped lines are not silently lost. At the end of each period in which
something was dropped, `throttle` says how much:

```
[throttle] dropped 1,204 lines in the last 1m
```

Periods are counted from when `throttle` started, so for `5/s` the summary
comes at most once a second and for `100/10m` at most once every ten
minutes. When input ends, a total follows if anything was dropped:

```
[throttle] total: dropped 3,204 of 5,004 lines over 12m4s
```

Summaries go to stderr so they never mix with the data, unless you ask for
`--summary stdout`, which is handy when the output is a terminal or a file
you'll read later.

### Keeping the newest line

With `--keep-last`, if the most recent line in a period was dropped, it is
emitted when the period ends (right after the summary), or when input ends.
A progress line or a "still failing" message is never left hidden behind
the limit. A kept line doesn't count as dropped.

### Sampling

Dropping everything over the limit means a long flood is represented only by
its first few lines. `--sample K` keeps a thin slice of the rest: once the
bucket is empty, every Kth line still gets through. With `throttle 100/s
--sample 50`, a stream of 10,000 lines a second comes out at about
100 + 9,900 / 50 ≈ 300 lines a second, spread across the whole flood. The
count of over-rate lines carries on from one period to the next, so a
steady overload is sampled steadily. Sampled lines don't count as dropped.

### Delay mode

`--delay` trades dropping for waiting: every line is queued and released at
the rate, evenly spaced (`--delay 10/s` writes one line every 100ms). The
burst defaults to 1 here so the spacing holds even after a quiet spell; pass
`--burst` to allow a quick start.

The queue holds at most `--buffer` lines. If input keeps arriving faster than
the rate and the queue fills up, the oldest queued line is dropped to make
room, so the output stays as current as it can; those drops are summarized
like any others. Use `--buffer 0` to queue without limit, for example when
replaying a file whose every line must come out. When input ends,
`throttle` keeps going until the queue is empty.

## Examples

```
tail -f app.log | throttle 30
journalctl -f | throttle 5/s --keep-last
./noisy-job 2>&1 | throttle 100 --per 10s --burst 20
kubectl logs -f deploy/api | throttle 60 --summary stdout > api.log
./load-test 2>&1 | throttle 100/s --sample 50
./replay-events < events.jsonl | throttle --delay 50/s --buffer 0 | ./slow-consumer
```

## License

MIT
