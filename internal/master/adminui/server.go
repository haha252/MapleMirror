package adminui

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"

	"mirror-server/internal/config"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/web"
)

const sessionCookie = "mirror_admin_session"
const adminAPIWriteBodyLimit int64 = 1 << 20

type Server struct {
	repo      mastercontrol.Repository
	syncStore mirrorsync.Store
	projects  *mirrorsync.ProjectLoader
	signer    func(*x509.CertificateRequest) (mastercontrol.SignedCertificate, error)
	sync      interface {
		Trigger(context.Context, string, string) (string, error)
	}
	indexNow interface {
		TriggerFullPublicNotification(context.Context) (int, error)
	}
	users                 map[string]userRecord
	store                 loginStore
	trustedCIDRs          []string
	timeLocation          *time.Location
	templates             *template.Template
	adminFS               fs.FS
	publicFS              fs.FS
	resetResourceLimiter  func(string)
	resetClientBlockCache func(string)
}

type Options struct {
	Projects *mirrorsync.ProjectLoader
	Signer   func(*x509.CertificateRequest) (mastercontrol.SignedCertificate, error)
	Sync     interface {
		Trigger(context.Context, string, string) (string, error)
	}
	IndexNow interface {
		TriggerFullPublicNotification(context.Context) (int, error)
	}
	TrustedCIDRs          []string
	Timezone              string
	ResetResourceLimiter  func(string)
	ResetClientBlockCache func(string)
}

func New(cfg config.Administration, repo mastercontrol.Repository, syncStore mirrorsync.Store, opts Options) (*Server, error) {
	users, err := loadUsers(cfg.Web.UsersFile, cfg.Web.BootstrapPasswordEnv)
	if err != nil {
		return nil, fmt.Errorf("读取管理面板用户文件失败：%w", err)
	}
	secret, err := loadOrCreateSecret(cfg.Web.SessionSecretFile)
	if err != nil {
		return nil, err
	}
	sessionTTL, err := parseWebDuration("admin.web.session_ttl", cfg.Web.SessionTTL)
	if err != nil {
		return nil, err
	}
	window, err := parseWebDuration("admin.web.login_failure_window", cfg.Web.LoginFailureWindow)
	if err != nil {
		return nil, err
	}
	banDuration, err := parseWebDuration("admin.web.login_ban_duration", cfg.Web.LoginBanDuration)
	if err != nil {
		return nil, err
	}
	timeLocation, err := loadTimeLocation(opts.Timezone)
	if err != nil {
		return nil, err
	}
	templates, err := template.ParseFS(web.Assets, "admin/templates/*.html")
	if err != nil {
		return nil, err
	}
	adminFS, err := fs.Sub(web.Assets, "admin/static")
	if err != nil {
		return nil, err
	}
	publicFS, err := fs.Sub(web.Assets, "public/static")
	if err != nil {
		return nil, err
	}
	return &Server{
		repo: repo, syncStore: syncStore, projects: opts.Projects,
		signer: opts.Signer, sync: opts.Sync, indexNow: opts.IndexNow, users: users,
		templates: templates, adminFS: adminFS, publicFS: publicFS,
		trustedCIDRs:          opts.TrustedCIDRs,
		timeLocation:          timeLocation,
		resetResourceLimiter:  opts.ResetResourceLimiter,
		resetClientBlockCache: opts.ResetClientBlockCache,
		store: loginStore{db: repo.DB, secret: secret, window: window,
			limit: cfg.Web.LoginFailureLimit, banDuration: banDuration, sessionTTL: sessionTTL},
	}, nil
}

func parseWebDuration(field, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("管理面板配置字段 %s 无效：%w", field, err)
	}
	return duration, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/admin/", http.StripPrefix("/static/admin/", http.FileServer(http.FS(s.adminFS))))
	mux.Handle("/static/public/", http.StripPrefix("/static/public/", http.FileServer(http.FS(s.publicFS))))
	mux.HandleFunc("/static/project-icons/", s.projectIcon)
	mux.HandleFunc("/admin/login", s.login)
	mux.HandleFunc("/admin/logout", s.requireSession(s.logout))
	mux.HandleFunc("/admin/api/overview", s.requireSession(s.overview))
	mux.HandleFunc("/admin/api/projects", s.requireSession(s.projectsAPI))
	mux.HandleFunc("/admin/api/projects/", s.requireSession(s.projectActionAPI))
	mux.HandleFunc("/admin/api/nodes", s.requireSession(s.nodesAPI))
	mux.HandleFunc("/admin/api/nodes/", s.requireSession(s.nodeActionAPI))
	mux.HandleFunc("/admin/api/sync/scans", s.requireSession(s.scanAPI))
	mux.HandleFunc("/admin/api/indexnow/submit", s.requireSession(s.indexNowAPI))
	mux.HandleFunc("/admin/api/sync/scans/latest", s.requireSession(s.latestScanAPI))
	mux.HandleFunc("/admin/api/sync/tasks", s.requireSession(s.syncTasksAPI))
	mux.HandleFunc("/admin/api/stats/overview", s.requireSession(s.statsOverviewAPI))
	mux.HandleFunc("/admin/api/stats/projects", s.requireSession(s.projectStatsAPI))
	mux.HandleFunc("/admin/api/authorizations/", s.requireSession(s.authorizationAPI))
	mux.HandleFunc("/admin/api/traffic/events", s.requireSession(s.trafficEventsAPI))
	mux.HandleFunc("/admin/api/pairing-codes", s.requireSession(s.pairingCodesAPI))
	mux.HandleFunc("/admin/api/pairing-requests", s.requireSession(s.pairingRequestsAPI))
	mux.HandleFunc("/admin/api/pairing-requests/", s.requireSession(s.pairingRequestActionAPI))
	mux.HandleFunc("/admin/api/security/blocks", s.requireSession(s.securityBlocksAPI))
	mux.HandleFunc("/admin/api/security/blocks/", s.requireSession(s.securityBlockActionAPI))
	mux.HandleFunc("/admin/api/security/audit-events", s.requireSession(s.auditEventsAPI))
	mux.HandleFunc("/admin/", s.requireSession(s.shell))
	return adminSecurityHeaders(mux)
}

func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		sessionToken := cookie.Value
		username, ok, err := s.store.verifySession(r.Context(), sessionToken, s.clientIP(r))
		if err != nil || !ok {
			clearSessionCookie(w)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		if adminAPIWrite(r) {
			r.Body = http.MaxBytesReader(w, r.Body, adminAPIWriteBodyLimit)
		}
		if !s.requireCSRF(w, r, sessionToken) {
			return
		}
		ctx := context.WithValue(r.Context(), usernameKey{}, username)
		ctx = context.WithValue(ctx, csrfTokenKey{}, s.csrfToken(sessionToken))
		next(w, r.WithContext(ctx))
	}
}

type usernameKey struct{}

func (s *Server) renderLogin(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "login.html", map[string]string{"Message": message})
}

func (s *Server) shell(w http.ResponseWriter, r *http.Request) {
	page, ok := adminPageForPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "shell.html", map[string]any{
		"Username":  r.Context().Value(usernameKey{}),
		"CSRFToken": r.Context().Value(csrfTokenKey{}),
		"Page":      page,
	})
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	data, err := s.overviewData(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "管理总览查询失败"})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}
