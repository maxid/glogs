[English](README.md) | 简体中文

# glogs

面向 Go 服务的结构化日志：命名通道、滚动文件、ANSI
颜色与请求上下文字段。

> 状态：**开发快照（未打标签）** — 核心可用；v1.0 之前 API 仍可能变化。
> 稳定版本发布前不发布标签；请使用 commit 伪版本（pseudo-version）。
> 许可证：MIT。

## 特性

- **命名通道**：`access`、`default`、`depend`、`app`、`error`、`event`
  （可配置；每个通道可拥有自己的文件与级别）。
- **滚动文件**：按大小 + 日期滚动，支持最大保留天数与总量上限，
  文件命名为 `<channel>.YYYY-MM-DD.<index>.log`。
- **ANSI 颜色**（自动/开/关，识别 `NO_COLOR`/`FORCE_COLOR`）。
- **结构化文本或 JSON** 输出。
- **上下文字段**：通过 `glogs.WithFields` 设置请求级属性。
- **`Stat` 助手**：`` `key=value ...` `` 形式的行。
- **HTTP 访问中间件**（`glogs/access`）：方法、URI、状态码、延迟、
  客户端 IP、trace id、可选的消息体捕获。

## 安装

```bash
go get github.com/maxid/glogs
```

## 快速开始

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

默认文件布局（相对于工作目录）：

```
logs/<appName>/common/access/access.log
logs/<appName>/common/default/default.log
logs/<appName>/common/depend/depend.log
logs/<appName>/common/app/app.log
logs/<appName>/common/error/error.log
logs/<appName>/common/event/event.log
```

每个通道的默认值：单文件 50 MiB、保留 7 天、总量上限 500 MiB。
`access` 同时镜像到控制台。

## 通道

| 通道 | 用途 |
|---|---|
| `access` | HTTP/请求访问日志 |
| `default` | 通用应用日志 |
| `depend` | 依赖/RPC 日志 |
| `app` | 业务/应用事件 |
| `error` | 错误（也可用于告警管道） |
| `event` | 生命周期/审计类事件 |

## 访问中间件

```go
handler := glogsaccess.Middleware(logger,
    glogsaccess.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/healthz" }),
    glogsaccess.WithBodyLimit(4096),
)(mux)
```

## 配置

```go
cfg := glogs.DefaultConfig("my-service")
cfg.Root = glogs.ConsoleConfig{Enabled: true, Format: glogs.FormatText, Color: glogs.ColorAuto}
cfg.Channels[glogs.ChannelAccess] = glogs.ChannelConfig{
    Enabled: true, Level: slog.LevelInfo,
    Path: "logs/my-service/common/access/access.log",
    Console: true, MaxSize: 50 << 20, MaxAge: 7, TotalCap: 500 << 20,
}
```

## 限制 / 路线图

- 异步缓冲写入、gzip 压缩、YAML 配置加载器与
  OpenTelemetry 自动注入均在计划中（见 [ROADMAP.md](ROADMAP.md)）。
- 基于数据库的审计日志不属于进程日志库的范围。

## 许可证

MIT。见 [LICENSE](LICENSE)。
