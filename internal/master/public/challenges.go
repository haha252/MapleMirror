package public

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const publicJSONBodyLimit = 16 * 1024

type altchaPayload struct {
	Challenge string `json:"challenge"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
	Number    int    `json:"number"`
}

func (s Server) webChallenge(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusGone, "WEB_PROTOCOL_RETIRED", "旧版网页验证协议已停用")
}

func (s Server) webAuthorize(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusGone, "WEB_PROTOCOL_RETIRED", "旧版网页验证协议已停用")
}

func (s Server) apiChallenge(w http.ResponseWriter, r *http.Request) {
	if !s.apiV1Enabled() {
		writeError(w, r, http.StatusGone, "API_VERSION_RETIRED", "公开 API V1 已停用")
		return
	}
	if s.rejectBlockedDownload(w, r, "", "api_challenge") {
		return
	}
	assetID, prefix, ok := s.challengeRequest(w, r)
	if !ok {
		return
	}
	_, bits, rejected := s.challengeAbuse(w, r, "api", prefix)
	if rejected {
		return
	}
	challenge, err := s.Store.CreatePoWChallenge(r.Context(), "api_pow", assetID, prefix, bits, s.APITTL)
	if err != nil {
		s.writeChallengeCreateError(w, r, err)
		return
	}
	writeOK(w, r, http.StatusCreated, "挑战已创建", map[string]any{
		"challenge_id": challenge.ID, "asset_id": assetID, "nonce_seed": challenge.Nonce,
		"algorithm": "sha256", "leading_zero_bits": challenge.Difficulty,
		"expires_at":       challenge.ExpiresAt,
		"canonical_format": "download.v1:{challenge_id}:{asset_id}:{nonce_seed}:{nonce}",
	})
}

func (s Server) apiAuthorize(w http.ResponseWriter, r *http.Request) {
	if !s.apiV1Enabled() {
		writeError(w, r, http.StatusGone, "API_VERSION_RETIRED", "公开 API V1 已停用")
		return
	}
	if s.rejectBlockedDownload(w, r, "", "api_authorization") {
		return
	}
	var in struct {
		ChallengeID string `json:"challenge_id"`
		AssetID     string `json:"asset_id"`
		Nonce       string `json:"nonce"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	s.authorize(w, r, challengeSubmit{SourceKind: "api", ProtocolVersion: "v1",
		Algorithm: "sha256", ChallengeID: in.ChallengeID, AssetID: in.AssetID, Solution: in.Nonce})
}

func (s Server) challengeRequest(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	var in struct {
		AssetID string `json:"asset_id"`
	}
	if !decodeJSON(w, r, &in) {
		return "", "", false
	}
	prefix := s.clientPrefix(r)
	if prefix == "unknown" {
		writeError(w, r, http.StatusBadRequest, "INVALID_CLIENT_SOURCE", "无法识别客户端来源")
		return "", "", false
	}
	return in.AssetID, prefix, true
}

func (s Server) challengeAbuse(w http.ResponseWriter, r *http.Request,
	source, prefix string) (abuseLevel, int, bool) {
	if s.AbuseTracker == nil {
		return abuseLevelNormal, 0, false
	}
	decision := s.AbuseTracker.recordAndDecide(source, prefix, 1, time.Now().UTC())
	if s.AbuseTracker.mode == "enforce" && decision.Level == abuseLevelReject {
		if decision.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())))
		}
		writeError(w, r, http.StatusTooManyRequests, "CLIENT_RATE_LIMITED", "请求过于频繁，请稍后再试")
		return decision.Level, 0, true
	}
	if s.AbuseTracker.mode != "enforce" {
		return abuseLevelNormal, 0, false
	}
	return decision.Level, decision.Bits, false
}

func (s Server) writeChallengeCreateError(w http.ResponseWriter, r *http.Request, err error) {
	switch err {
	case errChallengeQuota, errChallengeOutstanding:
		writeError(w, r, http.StatusTooManyRequests, "CHALLENGE_RATE_LIMITED", "挑战创建过于频繁，请稍后再试")
	case errChallengeCapacity, errVDFBusy:
		w.Header().Set("Retry-After", "2")
		code := "CHALLENGE_CAPACITY_REACHED"
		if err == errVDFBusy {
			code = "VDF_BUSY"
		}
		writeError(w, r, http.StatusServiceUnavailable, code, "挑战服务繁忙，请稍后再试")
	default:
		writeError(w, r, http.StatusConflict, "NO_ROUTABLE_NODE", "当前没有可用下载节点")
	}
}

func validLeadingZeros(c Challenge, solution string) bool {
	sum := sha256.Sum256([]byte("download.v1:" + c.ID + ":" + c.AssetID + ":" + c.Nonce + ":" + solution))
	return hasLeadingZeros(sum[:], c.Difficulty)
}

func hasLeadingZeros(sum []byte, bits int) bool {
	for _, value := range sum {
		if bits <= 0 {
			return true
		}
		if bits >= 8 {
			if value != 0 {
				return false
			}
			bits -= 8
			continue
		}
		return value>>(8-bits) == 0
	}
	return bits <= 0
}

func randomText(length int) string {
	buf := make([]byte, length)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return false
	}
	body := http.MaxBytesReader(w, r.Body, publicJSONBodyLimit)
	defer body.Close()
	if err := json.NewDecoder(body).Decode(out); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "INVALID_REQUEST", "请求内容过大")
			return false
		}
		writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不合法")
		return false
	}
	return true
}

func normalizeSolution(value string) string { return strings.TrimSpace(value) }

func (s Server) apiV1Enabled() bool { return s.APIV1Enabled == nil || *s.APIV1Enabled }
