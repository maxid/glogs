# glogs

Structured logging for Go services with named channels, rolling files, ANSI
colors and request context fields.

> Status: **v0.1.3** — usable core; API may still change before v1.0.
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
