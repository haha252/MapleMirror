package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mirror-server/internal/logging"
)

func startTLSListener(address string, tlsCfg *tls.Config, cfgErr error, logger *logging.Logger, handle func(net.Conn)) {
	if cfgErr != nil {
		logger.Error(context.Background(), "控制面 TLS 配置失败", slog.String("error", cfgErr.Error()))
		return
	}
	listener, err := tls.Listen("tcp", address, tlsCfg)
	if err != nil {
		logger.Error(context.Background(), "控制面监听启动失败", slog.String("listen", address), slog.String("error", err.Error()))
		return
	}
	go func() {
		logger.Info(context.Background(), "控制面 TLS 监听已启动", slog.String("listen", address))
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handle(conn)
		}
	}()
}

func runServer(address string, handler http.Handler, logger *logging.Logger) {
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.Info(context.Background(), "主节点健康服务已启动", slog.String("listen", address))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error(context.Background(), "主节点健康服务异常退出", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
