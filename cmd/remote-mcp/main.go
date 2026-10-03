package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"remote-mcp/internal/config"
	"remote-mcp/internal/server"
	"syscall"
)

func main() {
	c, err := config.Parse(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动失败：", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx, c, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "服务错误：", err)
		os.Exit(1)
	}
}
