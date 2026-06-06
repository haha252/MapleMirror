package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/controltls"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/admin"
	"mirror-server/internal/master/adminui"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/requestid"
)

func startAdminService(cfg config.Master, repo mastercontrol.Repository, syncService mirrorsync.Service, projectLoader *mirrorsync.ProjectLoader, logger *logging.Logger) {
	if cfg.Admin.TLS.CertFile == "" || cfg.Admin.TLS.KeyFile == "" {
		logger.Warn(context.Background(), "管理 API TLS 材料未配置，管理服务未启动")
		return
	}
	auth, err := admin.NewAuth(cfg.Admin)
	if err != nil {
		logger.Error(context.Background(), "管理 API 鉴权初始化失败", slog.String("error", err.Error()))
		return
	}
	if cfg.Node.TLS.SigningCACertFile == "" || cfg.Node.TLS.SigningCAKeyFile == "" {
		logger.Error(context.Background(), "节点证书签发 CA 未配置，管理服务未启动")
		return
	}
	loaded, err := mastercontrol.LoadCertificateSigner(
		cfg.Node.TLS.SigningCACertFile, cfg.Node.TLS.SigningCAKeyFile, 365*24*time.Hour)
	if err != nil {
		logger.Error(context.Background(), "节点证书签发器初始化失败", slog.String("error", err.Error()))
		return
	}
	handler := adminHandler(cfg, repo, syncService, projectLoader, logger, auth, loaded)
	tlsCfg, err := controltls.AdminServer(cfg.Admin.TLS.CertFile, cfg.Admin.TLS.KeyFile, cfg.Admin.TLS.ClientCAFile)
	if err != nil {
		logger.Error(context.Background(), "管理 API TLS 初始化失败", slog.String("error", err.Error()))
		return
	}
	server := &http.Server{
		Addr: cfg.Server.ManagementListen, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, TLSConfig: tlsCfg,
	}
	go func() {
		logger.Info(context.Background(), "管理 API 已启动", slog.String("listen", cfg.Server.ManagementListen))
		if err := server.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(context.Background(), "管理 API 异常退出", slog.String("error", err.Error()))
		}
	}()
}

func adminHandler(cfg config.Master, repo mastercontrol.Repository, syncService mirrorsync.Service, projectLoader *mirrorsync.ProjectLoader, logger *logging.Logger, auth admin.Auth, loaded mastercontrol.CertificateSigner) http.Handler {
	apiHandler := admin.Server{
		Auth: auth, Repo: repo, Signer: loaded.Sign,
		Sync: syncService, SyncStore: syncService.Scanner.Store, Projects: projectLoader, Logger: logger,
	}.Handler()
	handler := http.Handler(apiHandler)
	if cfg.Admin.Web.Enabled != nil && *cfg.Admin.Web.Enabled {
		ui, uiErr := adminui.New(cfg.Admin, repo, syncService.Scanner.Store, adminui.Options{
			Projects:     projectLoader,
			Signer:       loaded.Sign,
			Sync:         syncService,
			TrustedCIDRs: cfg.Proxy.TrustedCIDRs,
		})
		if uiErr != nil {
			logger.Error(context.Background(), "管理面板初始化失败", slog.String("error", uiErr.Error()))
		} else {
			mux := http.NewServeMux()
			if cfg.Admin.Web.ExclusiveAPI == nil || !*cfg.Admin.Web.ExclusiveAPI {
				mux.Handle("/api/admin/v1/", apiHandler)
			}
			mux.HandleFunc("/admin", adminEntry)
			mux.Handle("/admin/", ui.Handler())
			mux.Handle("/static/", ui.Handler())
			mux.HandleFunc("/", adminRoot)
			handler = mux
			logger.Info(context.Background(), "管理面板已启用", slog.String("path", "/admin/"))
		}
	}
	return requestid.Middleware(handler, cfg.RequestID.ResponseHeader, cfg.RequestID.ParentHeader)
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
