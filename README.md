# glogs

Structured logging for Go services with logback-like features used by XDMP:
named channels, rolling files, ANSI colors, text/JSON output and request
context fields.

> Status: **v0.1.1** — usable core; API may still change before v1.0.
> License: MIT.

## Features

- **Named channels**: `access`, `default`, `depend`, `app`, `error`, `event`
  (same set as the platform `logback-spring.xml`).
- **Rolling files**: size + date based rotation, max age and total size cap,
  file naming `<channel>.YYYY-MM-DD.<index>.log`.
- **ANSI colors** on console (auto/on/off, `NO_COLOR`/`FORCE_COLOR` aware).
- **Structured text or JSON** output.
- **Context fields**: request-scoped fields via `glogs.WithFields` (Go
  replacement for the Java `LogContext`/MDC).
- **`Stat` helper**: `` `key=value ...` `` lines (same shape as `CommonLog.stat()`).
- **HTTP access middleware** (`glogs/access`): method, URI, status, latency,
  client IP, trace ID, optional body capture.

## Install

```bash
go get github.com/maxid/glogs
```

## Quick start

```go
logger, err := glogs.New(glogs.DefaultConfig("xdmp-iot"))
if err != nil {
    panic(err)
}
defer logger.Close()

logger.Root().Info("console output")
logger.Channel(glogs.ChannelApp).Info("device online", "deviceKey", "dev-1")
logger.Channel(glogs.ChannelError).Error("connect failed", "err", err)

ctx := glogs.WithFields(context.Background(), slog.String("trace_id", "abc"))
logger.Channel(glogs.ChannelDefault).InfoContext(ctx, "with context fields")
```

Default file layout (relative to the working directory, matching the Java
platform):

```
logs/<appName>/common/access/access.log
logs/<appName>/common/default/default.log
logs/<appName>/common/depend/depend.log
logs/<appName>/common/app/app.log
logs/<appName>/common/error/error.log
logs/<appName>/common/event/event.log
```

Defaults per channel: 50 MiB per file, 7 days history, 500 MiB total cap.
`access` is also mirrored to the console (as in the Java configuration).

## Access middleware

```go
mux := http.NewServeMux()
handler := glogsaccess.Middleware(logger,
    glogsaccess.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/healthz" }),
    glogsaccess.WithBodyLimit(4096),
)(mux)
```

## Parity with `logback-spring.xml`

| logback / XDMP | glogs |
|---|---|
| `<logger name="access|default|depend|app|error|event">` | `Logger.Channel(...)` |
| `RollingFileAppender` + `SizeAndTimeBasedRollingPolicy` (50MB/7d/500MB) | `ChannelConfig{MaxSize, MaxAge, TotalCap}` (same defaults) |
| `${user.dir}/logs/${APP_NAME}/common/{channel}/{channel}.log` | default `Config` paths (same layout) |
| ANSI console pattern (`%clr`) | text handler with `ColorAuto/On/Off` |
| Root / class loggers → console only | `Logger.Root()` → console |
| `CommonLog.ACCESS` also to console | `access` channel `Console: true` by default |
| `LogContext` (ThreadLocal MDC) | `glogs.WithFields(ctx, attrs...)` |
| `CommonLog.stat()` `` `k=v` `` | `logger.Stat().Add(k, v).Log(l)` |
| `AccessLogFilter` / `AccessLogInterceptor` | `glogs/access.Middleware` |
| logback XML config | Go `Config` struct (YAML loader on roadmap) |
| Audit log / login log DB tables | out of scope (platform audit module) |

## Config

```go
cfg := glogs.DefaultConfig("xdmp-iot")
cfg.Root = glogs.ConsoleConfig{Enabled: true, Format: glogs.FormatText, Color: glogs.ColorAuto}
cfg.Channels[glogs.ChannelAccess] = glogs.ChannelConfig{
    Enabled: true, Level: slog.LevelInfo,
    Path: "logs/xdmp-iot/common/access/access.log",
    Console: true, MaxSize: 50 << 20, MaxAge: 7, TotalCap: 500 << 20,
}
```

## Limits / roadmap

- Async buffered appender, gzip compression, YAML config loader and
  OpenTelemetry auto-injection are planned (see [ROADMAP.md](ROADMAP.md)).
- Audit/operate/login DB logs remain a platform concern, not a process log.

## License

MIT. See [LICENSE](LICENSE).
