package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/health"
	"mirror-server/internal/requestid"
	"mirror-server/internal/storage"
)

var version = "开发版"

func main() {
	path := flag.String("config", "config.yaml", "主节点配置文件路径")
	projectsPath := flag.String("projects", "projects.yaml", "项目清单配置文件路径")
	quotaPath := flag.String("quota", "quota.yaml", "额度配置文件路径")
	flag.Parse()
	var warnings [][2]string
	warn := func(field, value string) { warnings = append(warnings, [2]string{field, value}) }
	var created bool
	cfg, err := config.LoadMaster(*path, warn)
	if handleLoad(err, "主节点配置", &created) {
		os.Exit(1)
	}
	if _, err := config.LoadProjects(*projectsPath, warn); handleLoad(err, "项目清单", &created) {
		os.Exit(1)
	}
	if _, err := config.LoadQuota(*quotaPath, warn); handleLoad(err, "额度配置", &created) {
		os.Exit(1)
	}
	if created {
		fmt.Fprintln(os.Stderr, "已生成主节点所需示例配置，请确认安全字段后重新启动。")
		return
	}
	location, _ := time.LoadLocation(cfg.Stats.Timezone)
	logger, err := logging.New("master", cfg.Logging, location, os.Stdout)
	if err != nil {
		logging.StartupError("主节点", err)
		os.Exit(1)
	}
	defer logger.Close()
	for _, item := range warnings {
		logger.ConfigWarning(item[0], item[1])
	}
	database, err := storage.OpenMaster(cfg.Database)
	if err != nil {
		logger.Error(context.Background(), "数据库初始化失败，主节点无法就绪", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer database.Close()
	logger.Info(context.Background(), "主节点数据库迁移已完成")
	mux := http.NewServeMux()
	mux.Handle("/healthz", requestid.Middleware(health.Handler{
		Logger: logger, Ready: func() bool { return true }, Version: version,
	}, cfg.RequestID.ResponseHeader, cfg.RequestID.ParentHeader))
	runServer(cfg.Server.PublicListen, mux, logger)
}

func handleLoad(err error, name string, created *bool) bool {
	if errors.Is(err, config.ErrExampleCreated) {
		*created = true
		return false
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s加载失败：%v\n", name, err)
		return true
	}
	return false
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
