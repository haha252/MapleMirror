package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/controltls"
	"mirror-server/internal/logging"
	nodecontrol "mirror-server/internal/node/control"
	"mirror-server/internal/node/health"
	"mirror-server/internal/requestid"
	"mirror-server/internal/storage"
)

var version = "开发版"

func main() {
	path := flag.String("config", "node.yaml", "下载节点配置文件路径")
	flag.Parse()
	var warnings [][2]string
	cfg, err := config.LoadNode(*path, func(field, value string) {
		warnings = append(warnings, [2]string{field, value})
	})
	if errors.Is(err, config.ErrExampleCreated) {
		fmt.Fprintln(os.Stderr, "已生成下载节点示例配置，请确认安全字段后重新启动。")
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "下载节点配置加载失败：%v\n", err)
		os.Exit(1)
	}
	location, _ := time.LoadLocation("Asia/Shanghai")
	logger, err := logging.New("node", cfg.Logging, location, os.Stdout)
	if err != nil {
		logging.StartupError("下载节点", err)
		os.Exit(1)
	}
	defer logger.Close()
	for _, item := range warnings {
		logger.ConfigWarning(item[0], item[1])
	}
	database, err := storage.OpenNode(cfg.Storage.StateDB)
	if err != nil {
		logger.Error(context.Background(), "节点状态库初始化失败，下载节点无法就绪", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer database.Close()
	logger.Info(context.Background(), "下载节点本地状态库迁移已完成")
	startControlClient(cfg, logger)
	mux := http.NewServeMux()
	mux.Handle("/healthz", requestid.Middleware(health.Handler{
		Logger: logger, Version: version,
	}, "X-Request-ID", "X-Request-ID"))
	runServer(cfg.Server.Listen, mux, logger)
}

func startControlClient(cfg config.Node, logger *logging.Logger) {
	if cfg.TLS.CAFile == "" || cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "" {
		logger.Warn(context.Background(), "节点控制证书材料未配置，控制连接未启动")
		return
	}
	tlsCfg, err := controltls.NodeClient(cfg.TLS.CAFile, cfg.TLS.CertFile, cfg.TLS.KeyFile, cfg.TLS.ServerName)
	if err != nil {
		logger.Error(context.Background(), "节点控制 TLS 初始化失败", slog.String("error", err.Error()))
		return
	}
	address, err := url.Parse(cfg.Master.ControlAddress)
	if err != nil {
		logger.Error(context.Background(), "主节点控制地址无效", slog.String("error", err.Error()))
		return
	}
	client := nodecontrol.Client{NodeID: cfg.Node.Name, Address: address.Host, TLSConfig: tlsCfg}
	stop := make(chan struct{})
	go client.Run(stop)
	logger.Info(context.Background(), "节点主动控制连接已启动", slog.String("master", address.Host))
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
	logger.Info(context.Background(), "下载节点健康服务已启动", slog.String("listen", address))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error(context.Background(), "下载节点健康服务异常退出", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
