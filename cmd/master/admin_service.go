package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/controltls"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/adminui"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/master/public"
	"mirror-server/internal/requestid"
)

func startAdminService(cfg config.Master, repo mastercontrol.Repository, syncService mirrorsync.Service,
	projectLoader *mirrorsync.ProjectLoader, publicServer *public.Server, logger *logging.Logger) {
	httpsEnabled := adminWebHTTPSEnabled(cfg)
	if httpsEnabled && (cfg.Admin.TLS.CertFile == "" || cfg.Admin.TLS.KeyFile == "") {
		logger.Warn(context.Background(), "管理面板 TLS 材料未配置，管理服务未启动")
		return
	}
	if cfg.Node.TLS.SigningCACertFile == "" || cfg.Node.TLS.SigningCAKeyFile == "" {
		logger.Error(context.Background(), "节点证书签发 CA 未配置，管理服务未启动")
		return
	}
	var err error
	loaded, err := mastercontrol.LoadCertificateSigner(
		cfg.Node.TLS.SigningCACertFile, cfg.Node.TLS.SigningCAKeyFile, 365*24*time.Hour)
	if err != nil {
		logger.Error(context.Background(), "节点证书签发器初始化失败", slog.String("error", err.Error()))
		return
	}
	handler, err := adminHandler(cfg, repo, syncService, projectLoader, publicServer, logger, loaded)
	if err != nil {
		logger.Error(context.Background(), "管理面板初始化失败", slog.String("error", err.Error()))
		return
	}
	var tlsCfg *tls.Config
	if httpsEnabled {
		tlsCfg, err = controltls.AdminWebServer(cfg.Admin.TLS.CertFile, cfg.Admin.TLS.KeyFile)
		if err != nil {
			logger.Error(context.Background(), "管理面板 TLS 初始化失败", slog.String("error", err.Error()))
			return
		}
	}
	server := &http.Server{
		Addr: cfg.Server.ManagementListen, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if httpsEnabled {
		server.TLSConfig = tlsCfg
	}
	go func() {
		scheme := "http"
		if httpsEnabled {
			scheme = "https"
		}
		logger.Info(context.Background(), "管理面板已启动",
			slog.String("listen", cfg.Server.ManagementListen), slog.String("scheme", scheme))
		if err := serveAdmin(server, httpsEnabled); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(context.Background(), "管理面板异常退出", slog.String("error", err.Error()))
		}
	}()
}

func adminWebHTTPSEnabled(cfg config.Master) bool {
	return cfg.Admin.Web.HTTPSEnabled == nil || *cfg.Admin.Web.HTTPSEnabled
}

func serveAdmin(server *http.Server, httpsEnabled bool) error {
	if httpsEnabled {
		return server.ListenAndServeTLS("", "")
	}
	return server.ListenAndServe()
}

func adminHandler(cfg config.Master, repo mastercontrol.Repository, syncService mirrorsync.Service,
	projectLoader *mirrorsync.ProjectLoader, publicServer *public.Server,
	logger *logging.Logger, loaded mastercontrol.CertificateSigner) (http.Handler, error) {
	var resetResourceLimiter func(string)
	if publicServer != nil {
		resetResourceLimiter = publicServer.ResetResourceLimiter
	}
	ui, err := adminui.New(cfg.Admin, repo, syncService.Scanner.Store, adminui.Options{
		Projects:             projectLoader,
		Signer:               loaded.Sign,
		Sync:                 syncService,
		TrustedCIDRs:         cfg.Proxy.TrustedCIDRs,
		Timezone:             cfg.Stats.Timezone,
		ResetResourceLimiter: resetResourceLimiter,
	})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/admin", adminEntry)
	mux.Handle("/admin/", ui.Handler())
	mux.Handle("/static/", ui.Handler())
	mux.HandleFunc("/", adminRoot)
	logger.Info(context.Background(), "管理面板挂载完成", slog.String("path", "/admin/"))
	return requestid.Middleware(mux, cfg.RequestID.ResponseHeader, cfg.RequestID.ParentHeader), nil
}

func adminRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func adminEntry(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}
