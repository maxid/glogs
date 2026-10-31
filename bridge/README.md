# glogs/bridge

Capture loggers that are not `slog`-based into glogs channels, so one process
writes one format, to one file set, under one redaction policy.

## What it can and cannot do

A bridge sees **lines**. It can attach a source label, sniff a level token and
bound the length; it cannot recover structured fields a library logged through its
own logger. When a library exposes a structured seam (slog, `zapcore.Core`), use
that instead — a line bridge is the fallback for dependencies that only offer an
`io.Writer`.

## Wiring a dependency

Most libraries accept an `io.Writer`:

```go
// logrus
logrus.SetOutput(bridge.NewWriter(logger, bridge.Options{Source: "logrus", SniffLevel: true}))

// zerolog
zl := zerolog.New(bridge.NewWriter(logger, bridge.Options{Source: "zerolog"}))

// zap (zapcore.AddSync accepts any io.Writer)
core := zapcore.NewCore(enc, zapcore.AddSync(bridge.NewWriter(logger, bridge.Options{Source: "zap"})), level)
zw := zap.New(core)
```

The standard library needs one call for every package that uses `log.Printf`:

```go
restore := bridge.Stdlib(logger, bridge.Options{Channel: glogs.ChannelDepend, Source: "stdlib"})
defer restore()
```

`Stdlib` clears the stdlib flags: the bridge writes its own timestamp, and a
prefix would duplicate it.

## Rules

- A write may split a line; the writer buffers until it sees `\n`. **`Close`
  flushes the trailing partial line** — a library that never emits a final newline
  would otherwise lose its last message.
- Empty lines are skipped.
- `MaxLineBytes` (default 8 KiB) truncates with a `...(truncated)` marker: a
  dependency that dumps a payload must not fill the log file.
- `SniffLevel` reads `INFO`/`WARN`/`ERROR`/`DEBUG` at the start of a line or inside
  `[...]`; without it every line is written at the configured level.
- Redaction is the channel handler's job, so it applies to bridged records exactly
  as it does to native ones. The **message text is not redacted** — a library that
  prints a credential inside its message cannot be caught by attribute masking.
  That is the reason to prefer a native adapter when one exists.
