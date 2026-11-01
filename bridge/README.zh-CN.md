[English](README.md) | 简体中文

# glogs/bridge

把非 `slog` logger 捕获到 glogs 通道，使一个进程只写一种格式、一组文件、一套脱敏策略。

## 能做什么、不能做什么

bridge 看到的是**行**。它可以附加 source 标签、嗅探 level token、限制长度；但无法恢复库通过自己 logger 记录的结构化字段。库若提供结构化接缝（slog、`zapcore.Core`），优先用那个；行 bridge 只是只有 `io.Writer` 的依赖的回退。

## 接入依赖

多数库接受 `io.Writer`：

```go
// logrus
logrus.SetOutput(bridge.NewWriter(logger, bridge.Options{Source: "logrus", SniffLevel: true}))

// zerolog
zl := zerolog.New(bridge.NewWriter(logger, bridge.Options{Source: "zerolog"}))

// zap（zapcore.AddSync 接受任意 io.Writer）
core := zapcore.NewCore(enc, zapcore.AddSync(bridge.NewWriter(logger, bridge.Options{Source: "zap"})), level)
zw := zap.New(core)
```

标准库需要为每个使用 `log.Printf` 的包调用一次：

```go
restore := bridge.Stdlib(logger, bridge.Options{Channel: glogs.ChannelDepend, Source: "stdlib"})
defer restore()
```

`Stdlib` 会清空 stdlib flags：bridge 自己写时间戳，前缀会重复。

## 规则

- 一次 write 可能拆行；writer 缓冲到看见 `\n`。**`Close` 会 flush 尾部不完整行**，否则从不输出最终换行的库会丢掉最后一条消息。
- 空行跳过。
- `MaxLineBytes`（默认 8 KiB）截断并加 `...(truncated)` 标记：转储 payload 的依赖不能撑满日志。
- `SniffLevel` 读取行首或 `[...]` 内的 `INFO`/`WARN`/`ERROR`/`DEBUG`；不开则所有行用配置的 level。
- 脱敏由 channel handler 负责，因此对 bridge 记录与原生记录同样生效。**消息正文不脱敏**：库把凭据打进消息时，属性 masking 抓不到。这也是优先原生 adapter 的原因。
