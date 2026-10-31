English | [简体中文](README.zh-CN.md)

# glogs

Structured logging for Go services with named channels, rolling files, ANSI
colors and request context fields.

> Status: **development snapshot (untagged)** — usable core; API may still change before v1.0.
> No tags are published until a stable release; use commit pseudo-versions.
> License: MIT.

## Features

- **Named channels**: `access`, `default`, `depend`, `app`, `error`, `event`
  (configurable; each channel can have its own file and level).
- **Rolling files**: size + date based rotation, max age and total size cap,
  file naming `<channel>.YYYY-MM-DD.<index>.log`.
- **ANSI colors** on console (auto/on/off, `NO_COLOR`/`FORCE_COLOR` aware).
- **Structured text or JSON** output.
- **Context fields**: request-scoped attributes via `glogs.WithFields`.
- **`Stat` helper**: `` `key=value ...` `` lines.
- **HTTP access middleware** (`glogs/access`): method, URI, status, latency,
  client IP, trace id, optional body capture.

## Bridging other loggers

Third-party packages rarely log through `slog`. `glogs/bridge` turns their output
into channel records so one process keeps one format, one file set and one
redaction policy:

```go
// logrus / zerolog / zap, or anything that writes to an io.Writer
logrus.SetOutput(bridge.NewWriter(logger, bridge.Options{Source: "logrus", SniffLevel: true}))

// the standard library: one call covers every package that calls log.Printf
restore := bridge.Stdlib(logger, bridge.Options{Channel: glogs.ChannelDepend, Source: "stdlib"})
defer restore()
```

A bridge sees **lines**, so it cannot recover structured fields; prefer a native
adapter when a library offers one (`slog`, `zapcore.Core`, a logrus `Hook`). See
[`bridge/README.md`](bridge/README.md) for the rules, including the one that
matters most: **message text is not redacted**, only attributes are.

## Failure visibility

`slog` discards the error a handler returns, so a full disk used to lose lines with
no signal. A channel now reports it two ways:

```go
logger, _ := glogs.New(glogs.Config{
    // ...
    OnError: func(channel glogs.Channel, err error) { alert(channel, err) },
})

stats, _ := logger.FileStats(glogs.ChannelApp)
// stats.Errors, stats.LastErr, stats.Bytes
```

`FileStats(file).Errors > 0` with a small `Bytes` is the signature of a broken disk
rather than a quiet service.

## Redaction

Nothing masks values unless you ask. A logged credential lives in the file forever,
so production should set a redactor:

```go
logger, err := glogs.New(glogs.Config{
    AppName:  "xdmp",
    Redactor: glogs.NewMaskRedactor("", "deviceSecret"), // "" = default mask
})
```

It matches attribute keys case-insensitively against a deny list
(`password`, `*token*`, `authorization`, `cookie`, `idCard`, … plus extra keys) and
masks JSON bodies field by field at any depth. Groups are walked too, so
`slog.Group("auth", "accessToken", …)` is covered.

The access middleware applies the same policy to captured bodies:

```go
access.Middleware(logger,
    access.WithBodyLimit(1024),
    access.WithBodyRedactor(func(body string) string {
        return redactor.RedactBody([]byte(body), false)
    }),
    access.WithLoggableContentTypes("application/json", "text/"),
)
```

Bodies are only captured for allow-listed content types; an undeclared or binary
type is skipped rather than guessed at.

## Async writes

By default a log line is written synchronously: a slow disk delays the request that
logged it. Set `Async` on a channel to move the write off the caller's goroutine:

```go
Channels: map[glogs.Channel]glogs.ChannelConfig{
    glogs.ChannelApp: {Enabled: true, Async: true, AsyncBuffer: 4096, Path: "logs/app.log"},
}
```

The queue is a **hard bound**. When it is full the line is dropped and counted —
never queued without limit, never blocking the request. That trade-off is visible:

```go
stats, err := logger.AsyncStats(glogs.ChannelApp)
// stats.Dropped, stats.Errors, stats.LastErr, stats.Pending
```

`Close` drains the queue (bounded by a 5s timeout) before closing the files, so
shutdown does not silently discard accepted lines.

## Install

```bash
go get github.com/maxid/glogs
```

## Quick start

```go
logger, err := glogs.New(glogs.DefaultConfig("my-service"))
if err != nil {
    panic(err)
}
defer logger.Close()

logger.Root().Info("console output")
logger.Channel(glogs.ChannelApp).Info("app event", "deviceKey", "dev-1")
logger.Channel(glogs.ChannelError).Error("connect failed", "err", err)

ctx := glogs.WithFields(context.Background(), slog.String("trace_id", "abc"))
logger.Channel(glogs.ChannelDefault).InfoContext(ctx, "with context fields")
```

Default file layout (relative to the working directory):

```
logs/<appName>/common/access/access.log
logs/<appName>/common/default/default.log
logs/<appName>/common/depend/depend.log
logs/<appName>/common/app/app.log
logs/<appName>/common/error/error.log
logs/<appName>/common/event/event.log
```

Defaults per channel: 50 MiB per file, 7 days history, 500 MiB total cap.
`access` is also mirrored to the console.

## Channels

| Channel | Purpose |
|---|---|
| `access` | HTTP/request access logs |
| `default` | general application logs |
| `depend` | dependency/RPC logs |
| `app` | business/application events |
| `error` | errors (also usable for alerting pipelines) |
| `event` | lifecycle/audit-style events |

## Access middleware

```go
handler := glogsaccess.Middleware(logger,
    glogsaccess.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/healthz" }),
    glogsaccess.WithBodyLimit(4096),
)(mux)
```

## Config

```go
cfg := glogs.DefaultConfig("my-service")
cfg.Root = glogs.ConsoleConfig{Enabled: true, Format: glogs.FormatText, Color: glogs.ColorAuto}
cfg.Channels[glogs.ChannelAccess] = glogs.ChannelConfig{
    Enabled: true, Level: slog.LevelInfo,
    Path: "logs/my-service/common/access/access.log",
    Console: true, MaxSize: 50 << 20, MaxAge: 7, TotalCap: 500 << 20,
}
```

## Limits / roadmap

- Async buffered appender, gzip compression, YAML config loader and
  OpenTelemetry auto-injection are planned (see [ROADMAP.md](ROADMAP.md)).
- Database-backed audit logs are out of scope for a process logger.

## License

MIT. See [LICENSE](LICENSE).
