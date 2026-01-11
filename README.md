# throttle

Limits output to N lines per minute.

Pipe a noisy stream through `throttle` and at most N lines per minute come
out the other side; the rest are dropped. Handy for chatty logs, runaway
debug output, or anything you want to watch without drowning in it.

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
| `--burst N`      | Lines that may pass at once after a quiet spell (default: the count). |
| `-h`, `--help`   | Show help.                                                      |
| `--version`      | Print the version.                                              |

Flags can go before or after the rate.

### How the limit works

`throttle` uses a token bucket. The bucket holds `--burst` tokens and starts
full; each line that passes spends one, and tokens come back at the rate
(for `30`, one every 2 seconds). By default the burst equals the count, so a
stream that has been quiet can let a full period's worth of lines through at
once, then settles to the steady rate.

When input ends, the number of dropped lines is reported on stderr.

## Examples

```
tail -f app.log | throttle 30
journalctl -f | throttle 5/s
./noisy-job 2>&1 | throttle 100 --per 10s --burst 20
```

## License

MIT
