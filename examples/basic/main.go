package main

import (
	"log/slog"

	"github.com/maxid/glogs"
)

func main() {
	cfg := glogs.DefaultConfig("xdmp-demo")
	logger, err := glogs.New(cfg)
	if err != nil {
		panic(err)
	}
	defer logger.Close()

	logger.Root().Info("console message")
	logger.Channel(glogs.ChannelApp).Info("app message", "device", "dev-1")
	logger.Channel(glogs.ChannelError).Error("error message")
	ctx := glogs.WithFields(nil, slog.String("trace_id", "demo-trace"))
	logger.Channel(glogs.ChannelDefault).InfoContext(ctx, "with fields")
}
