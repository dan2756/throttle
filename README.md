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
tail -f app.log | throttle 30
```

`throttle` uses a token bucket: a quiet stream can burst a full minute's
worth of lines at once, after which lines pass at a steady 30 per minute.
When input ends, the number of dropped lines is reported on stderr.

## License

MIT
