package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
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

func runServer(address string, handler http.Handler, fatal <-chan error, logger *logging.Logger) error {
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdown := func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}
	serveErr := make(chan error, 1)
	logger.Info(context.Background(), "主节点健康服务已启动", slog.String("listen", address))
	go func() { serveErr <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdown()
		return nil
	case err := <-fatal:
		shutdown()
		return err
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("主节点健康服务异常退出：%w", err)
	}
}
