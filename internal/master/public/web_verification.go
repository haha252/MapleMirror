package public

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"
)

const webVerificationBodyLimit int64 = 4 << 10

type webVerificationButton struct {
	Label  string
	Intent string
}

var webVerificationLabelGroups = [][]string{
	{"立即验证", "开始验证", "继续验证", "开始安全检查", "继续人机验证"},
	{"我是人类", "确认我是人类", "确认人类身份", "验证人类身份", "通过人机检查"},
	{"完成验证", "验证并继续", "完成后继续", "验证并下载", "完成安全检查"},
}

func (s Server) webVerification(w http.ResponseWriter, r *http.Request) {
	mode := s.webVerificationMode()
	if mode == "off" || r.Method != http.MethodPost || s.WebVerifications == nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, webVerificationBodyLimit)
	if err := r.ParseForm(); err != nil {
		http.NotFound(w, r)
		return
	}
	token := strings.TrimSpace(r.PostForm.Get("verification_token"))
	assetID := strings.TrimSpace(r.PostForm.Get("asset_id"))
	prefix := s.clientPrefix(r)
	if !s.WebVerifications.consume(token, assetID, prefix, time.Now().UTC()) {
		http.NotFound(w, r)
		return
	}

	exempt := s.webVerificationExempt(prefix)
	if s.Logger != nil {
		s.Logger.Warn(r.Context(), "网页验证异常操作已确认",
			slog.String("request_id", requestID(r)),
			slog.String("asset_id", assetID),
			slog.String("client_source", fullPublicSource(s.clientIP(r))),
			slog.String("mode", mode),
			slog.Bool("exempt", exempt))
	}
	if mode != "enforce" || exempt {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.Store.upsertAutoBlock(r.Context(), prefix, "automated_access",
		"local_auto_ban", time.Now().UTC(), s.blocklistAutoBanDuration()); err != nil {
		if s.Logger != nil {
			s.Logger.Warn(r.Context(), "网页验证异常操作封禁写入失败",
				slog.String("request_id", requestID(r)),
				slog.String("asset_id", assetID),
				slog.String("client_source", fullPublicSource(s.clientIP(r))),
				slog.String("error", err.Error()))
		}
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "请求处理失败")
		return
	}
	if s.ClientBlocks != nil {
		s.ClientBlocks.invalidate(prefix)
	}
	writeJSON(w, http.StatusForbidden, response{
		Status:    "error",
		Code:      "CLIENT_BLOCKED",
		Message:   "当前来源已被限制访问",
		RequestID: requestID(r),
		Data: map[string]string{
			"source": fullPublicSource(s.clientIP(r)),
		},
	})
}

func (s Server) webVerificationMode() string {
	if s.AbuseTracker == nil {
		return "off"
	}
	switch strings.ToLower(strings.TrimSpace(s.AbuseTracker.mode)) {
	case "monitor":
		return "monitor"
	case "enforce":
		return "enforce"
	default:
		return "off"
	}
}

func (s Server) webVerificationExempt(clientPrefix string) bool {
	return (s.Blocklist != nil && s.Blocklist.exempt(clientPrefix)) ||
		s.Store.Quota.exempt(clientPrefix)
}

func newWebVerificationButtons() []webVerificationButton {
	return webVerificationButtons(cryptorand.Reader)
}

func webVerificationButtons(reader io.Reader) []webVerificationButton {
	buttons := make([]webVerificationButton, 0, len(webVerificationLabelGroups))
	for _, group := range webVerificationLabelGroups {
		index, err := secureRandomIndex(reader, len(group))
		if err != nil {
			return fallbackWebVerificationButtons()
		}
		intent, err := randomVerificationIntent(reader)
		if err != nil {
			return fallbackWebVerificationButtons()
		}
		buttons = append(buttons, webVerificationButton{Label: group[index], Intent: intent})
	}
	for i := len(buttons) - 1; i > 0; i-- {
		index, err := secureRandomIndex(reader, i+1)
		if err != nil {
			return fallbackWebVerificationButtons()
		}
		buttons[i], buttons[index] = buttons[index], buttons[i]
	}
	return buttons
}

func secureRandomIndex(reader io.Reader, limit int) (int, error) {
	value, err := cryptorand.Int(reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}

func randomVerificationIntent(reader io.Reader) (string, error) {
	value := make([]byte, 12)
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func fallbackWebVerificationButtons() []webVerificationButton {
	return []webVerificationButton{
		{Label: "立即验证", Intent: "continue"},
		{Label: "确认我是人类", Intent: "confirm"},
		{Label: "完成验证", Intent: "complete"},
	}
}
