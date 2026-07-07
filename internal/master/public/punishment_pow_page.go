package public

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"html/template"
)

type punishmentPowView struct {
	ChallengeSeed string      `json:"challenge_seed"`
	Difficulty    int         `json:"difficulty"`
	RequestID     string      `json:"request_id"`
	ServerTime    string      `json:"server_time"`
	Source        string      `json:"source"`
	Email         string      `json:"email"`
	WorkerLimit   int         `json:"worker_limit"`
	Payload       template.JS `json:"-"`
}

func (s Server) renderPunishmentPage(w http.ResponseWriter, r *http.Request, decision blockDecision) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	view := punishmentPowView{
		ChallengeSeed: punishmentChallengeSeed(),
		Difficulty:    s.punishmentDifficulty(),
		RequestID:     requestID(r),
		ServerTime:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        fullPublicSource(s.clientIP(r)),
		Email:         "frostlynx@qq.com",
		WorkerLimit:   s.punishmentWorkerLimit(),
	}
	body, err := json.Marshal(view)
	if err != nil {
		http.Error(w, "验证页面渲染失败", http.StatusInternalServerError)
		return
	}
	view.Payload = template.JS(body)
	rendered, err := s.renderTemplateBody("punishment_pow", view)
	if err != nil {
		http.Error(w, "验证页面渲染失败", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, pageData{
		Title:        "访问受限验证",
		BrowserTitle: "访问受限验证 - 枫源镜像",
		Description:  "访问受限验证页面",
		BodyClass:    "page-punishment-pow",
		HideHeader:   true,
		Body:         rendered,
		Styles:       []string{"/static/public/download-pow.css", "/static/public/punishment-pow.css"},
		Scripts:      []string{"/static/public/pow-loader.js", "/static/public/punishment-pow.js"},
		StatusCode:   http.StatusForbidden,
	})
}

func punishmentChallengeSeed() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func (s Server) punishmentDifficulty() int {
	if s.AbuseTracker == nil || s.AbuseTracker.cfg.Punishment.Difficulty <= 0 {
		return 128
	}
	return s.AbuseTracker.cfg.Punishment.Difficulty
}

func (s Server) punishmentWorkerLimit() int {
	if s.AbuseTracker == nil || s.AbuseTracker.cfg.Punishment.WorkerLimit <= 0 {
		return 32
	}
	return s.AbuseTracker.cfg.Punishment.WorkerLimit
}

func fullPublicSource(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "unknown" {
		return "unknown"
	}
	var addr netip.Addr
	if prefix, err := netip.ParsePrefix(value); err == nil {
		if prefix.Bits() != prefix.Addr().BitLen() {
			return prefix.Masked().String()
		}
		addr = prefix.Addr()
	} else if parsed, err := netip.ParseAddr(value); err == nil {
		addr = parsed
	}
	if !addr.IsValid() {
		return "unknown"
	}
	return addr.String()
}
