package adminui

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"time"

	"mirror-server/internal/config"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/web"
)

const sessionCookie = "mirror_admin_session"

type Server struct {
	repo      mastercontrol.Repository
	syncStore mirrorsync.Store
	projects  *mirrorsync.ProjectLoader
	users     map[string]userRecord
	store     loginStore
	networks  []*net.IPNet
	templates *template.Template
	adminFS   fs.FS
	publicFS  fs.FS
}

func New(cfg config.Administration, repo mastercontrol.Repository, syncStore mirrorsync.Store, projects *mirrorsync.ProjectLoader) (*Server, error) {
	users, err := loadUsers(cfg.Web.UsersFile)
	if err != nil {
		return nil, fmt.Errorf("读取管理面板用户文件失败：%w", err)
	}
	secret, err := loadOrCreateSecret(cfg.Web.SessionSecretFile)
	if err != nil {
		return nil, err
	}
	sessionTTL, _ := time.ParseDuration(cfg.Web.SessionTTL)
	window, _ := time.ParseDuration(cfg.Web.LoginFailureWindow)
	banDuration, _ := time.ParseDuration(cfg.Web.LoginBanDuration)
	networks, err := parseNetworks(cfg.AllowedCIDRs)
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
		repo: repo, syncStore: syncStore, projects: projects, users: users, networks: networks,
		templates: templates, adminFS: adminFS, publicFS: publicFS,
		store: loginStore{db: repo.DB, secret: secret, window: window,
			limit: cfg.Web.LoginFailureLimit, banDuration: banDuration, sessionTTL: sessionTTL},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/admin/", http.StripPrefix("/static/admin/", http.FileServer(http.FS(s.adminFS))))
	mux.Handle("/static/public/", http.StripPrefix("/static/public/", http.FileServer(http.FS(s.publicFS))))
	mux.HandleFunc("/admin/login", s.login)
	mux.HandleFunc("/admin/logout", s.logout)
	mux.HandleFunc("/admin/api/overview", s.requireSession(s.overview))
	mux.HandleFunc("/admin/api/projects", s.requireSession(s.projectsAPI))
	mux.HandleFunc("/admin/", s.requireSession(s.shell))
	return s.networkGuard(mux)
}

func (s *Server) networkGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowedRemote(r.RemoteAddr) {
			http.Error(w, "管理来源暂不可用", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowedRemote(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range s.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func parseNetworks(values []string) ([]*net.IPNet, error) {
	var networks []*net.IPNet
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, err
		}
		networks = append(networks, network)
	}
	return networks, nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := remoteIP(r)
	blocked, err := s.store.blocked(r.Context(), ip)
	if err != nil {
		http.Error(w, "管理面板暂不可用", http.StatusInternalServerError)
		return
	}
	if blocked.Blocked {
		s.renderLogin(w, "登录失败或当前来源暂不可用")
		return
	}
	if r.Method == http.MethodGet {
		s.renderLogin(w, "")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "接口不存在", http.StatusNotFound)
		return
	}
	_ = r.ParseForm()
	user, ok := s.users[r.Form.Get("username")]
	if !ok || !verifyPassword(user.PasswordHash, r.Form.Get("password")) {
		_ = s.store.recordFailure(r.Context(), ip)
		s.renderLogin(w, "登录失败或当前来源暂不可用")
		return
	}
	token, expires, err := s.store.createSession(r.Context(), user.Username, ip)
	if err != nil {
		http.Error(w, "会话创建失败", http.StatusInternalServerError)
		return
	}
	s.store.clearFailures(r.Context(), ip)
	setSessionCookie(w, token, expires)
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.store.deleteSession(r.Context(), cookie.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		username, ok, err := s.store.verifySession(r.Context(), cookie.Value, remoteIP(r))
		if err != nil || !ok {
			clearSessionCookie(w)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), usernameKey{}, username)))
	}
}

type usernameKey struct{}

func (s *Server) renderLogin(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "login.html", map[string]string{"Message": message})
}

func (s *Server) shell(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "shell.html", map[string]any{
		"Username": r.Context().Value(usernameKey{}),
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

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func setSessionCookie(w http.ResponseWriter, token, expires string) {
	exp, _ := time.Parse(time.RFC3339Nano, expires)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/admin",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, Expires: exp})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/admin",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
		Expires: time.Unix(0, 0), MaxAge: -1})
}

type dbQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
