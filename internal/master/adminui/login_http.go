package adminui

import "net/http"

const adminLoginBodyLimit int64 = 16 << 10

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if ip == "unknown" {
		http.Error(w, "无法识别管理来源", http.StatusBadRequest)
		return
	}
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
	r.Body = http.MaxBytesReader(w, r.Body, adminLoginBodyLimit)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "请求内容不合法", http.StatusBadRequest)
		return
	}
	user, ok := s.users[r.Form.Get("username")]
	passwordHash := user.PasswordHash
	if !ok {
		for _, candidate := range s.users {
			passwordHash = candidate.PasswordHash
			break
		}
	}
	passwordOK := verifyPassword(passwordHash, r.Form.Get("password"))
	if !ok || !passwordOK {
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
	if r.Method != http.MethodPost {
		http.Error(w, "接口不存在", http.StatusNotFound)
		return
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.store.deleteSession(r.Context(), cookie.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
