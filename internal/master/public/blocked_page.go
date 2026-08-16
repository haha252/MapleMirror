package public

import (
	"net/http"
	"time"
)

type blockedPageView struct {
	RequestID  string
	ServerTime string
	IP         string
	Email      string
}

func (s Server) renderBlockedPage(w http.ResponseWriter, r *http.Request, _ blockDecision) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	view := blockedPageView{
		RequestID:  requestID(r),
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
		IP:         fullPublicSource(s.clientIP(r)),
		Email:      "frostlynx@qq.com",
	}
	rendered, err := s.renderTemplateBody("blocked", view)
	if err != nil {
		http.Error(w, "封禁页面渲染失败", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, pageData{
		Title:        "访问已被限制",
		BrowserTitle: "访问已被限制 - 枫源镜像",
		Description:  "访问受限提示页面",
		Robots:       noIndexRobots,
		BodyClass:    "page-blocked page-punishment-pow",
		HideHeader:   true,
		Body:         rendered,
		Styles:       []string{"/static/public/download-pow.css", "/static/public/punishment-pow.css"},
		StatusCode:   http.StatusForbidden,
	})
}
