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
	if s.rejectBlockedDownload(w, r, "", "web_challenge") {
		return
	}
	var in struct {
		AssetID string `json:"asset_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	prefix := s.clientPrefix(r)
	now := time.Now().UTC()
	difficulty := s.ALTCHADifficulty
	if s.AbuseTracker != nil {
		decision := s.AbuseTracker.recordAndDecide("web", prefix, 1, now)
		if s.AbuseTracker.mode == "enforce" && decision.Level == abuseLevelReject {
			if decision.RetryAfter > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())))
			}
			writeError(w, r, http.StatusTooManyRequests, "CLIENT_RATE_LIMITED", "请求过于频繁，请稍后再试")
			return
		}
		if s.AbuseTracker.mode == "enforce" && decision.Bits > 0 {
			difficulty = minInt(difficulty+decision.Bits, s.AbuseTracker.cfg.Challenge.MaxBits)
		}
	}
	if difficulty <= 0 {
		difficulty = s.ALTCHADifficulty
	}
	challenge, err := s.Store.CreateChallenge(r.Context(), "altcha", in.AssetID,
		prefix, difficulty, s.ALTCHATTL, requestID(r))
	if err != nil {
		if err == errChallengeQuota {
			writeError(w, r, http.StatusTooManyRequests, "CHALLENGE_RATE_LIMITED", "挑战创建过于频繁，请稍后再试")
			return
		}
		writeError(w, r, http.StatusConflict, "NO_ROUTABLE_NODE", "当前没有可用下载节点")
		return
	}
	payload := s.altchaPayload(challenge)
	writeOK(w, r, http.StatusCreated, "挑战已创建", map[string]any{
		"challenge_id": challenge.ID, "altcha": payload, "difficulty": challenge.Difficulty,
		"expires_at": challenge.ExpiresAt,
	})
}

func (s Server) apiChallenge(w http.ResponseWriter, r *http.Request) {
	if s.rejectBlockedDownload(w, r, "", "api_challenge") {
		return
	}
	var in struct {
		AssetID string `json:"asset_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	prefix := s.clientPrefix(r)
	now := time.Now().UTC()
	difficulty := s.APIZeroBits
	if s.AbuseTracker != nil {
		decision := s.AbuseTracker.recordAndDecide("api", prefix, 1, now)
		if s.AbuseTracker.mode == "enforce" && decision.Level == abuseLevelReject {
			if decision.RetryAfter > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())))
			}
			writeError(w, r, http.StatusTooManyRequests, "CLIENT_RATE_LIMITED", "请求过于频繁，请稍后再试")
			return
		}
		if s.AbuseTracker.mode == "enforce" && decision.Bits > 0 {
			difficulty = minInt(difficulty+decision.Bits, s.AbuseTracker.cfg.Challenge.MaxBits)
		}
	}
	challenge, err := s.Store.CreateChallenge(r.Context(), "api_pow", in.AssetID,
		prefix, difficulty, s.APITTL, requestID(r))
	if err != nil {
		if err == errChallengeQuota {
			writeError(w, r, http.StatusTooManyRequests, "CHALLENGE_RATE_LIMITED", "挑战创建过于频繁，请稍后再试")
			return
		}
		writeError(w, r, http.StatusConflict, "NO_ROUTABLE_NODE", "当前没有可用下载节点")
		return
	}
	writeOK(w, r, http.StatusCreated, "挑战已创建", map[string]any{
		"challenge_id": challenge.ID, "asset_id": in.AssetID, "nonce_seed": challenge.Nonce,
		"algorithm": "sha256", "leading_zero_bits": challenge.Difficulty,
		"expires_at":       challenge.ExpiresAt,
		"canonical_format": "download.v1:{challenge_id}:{asset_id}:{nonce_seed}:{nonce}",
	})
}

func (s Server) webAuthorize(w http.ResponseWriter, r *http.Request) {
	if s.rejectBlockedDownload(w, r, "", "web_authorization") {
		return
	}
	var in struct {
		ChallengeID   string          `json:"challenge_id"`
		AssetID       string          `json:"asset_id"`
		AltchaPayload json.RawMessage `json:"altcha_payload"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	var payload altchaPayload
	if err := json.Unmarshal(in.AltchaPayload, &payload); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "网页挑战提交内容不合法")
		return
	}
	s.authorize(w, r, challengeSubmit{Kind: "altcha", ChallengeID: in.ChallengeID,
		AssetID: in.AssetID, Solution: strconv.Itoa(payload.Number)})
}

func (s Server) apiAuthorize(w http.ResponseWriter, r *http.Request) {
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
	s.authorize(w, r, challengeSubmit{Kind: "api_pow", ChallengeID: in.ChallengeID,
		AssetID: in.AssetID, Solution: in.Nonce})
}

func (s Server) altchaPayload(c Challenge) altchaPayload {
	salt := randomText(12)
	return altchaPayload{Challenge: c.Nonce, Salt: salt, Signature: s.Signer.Signature(c.Nonce + ":" + salt)}
}

func validLeadingZeros(c Challenge, solution string) bool {
	sum := sha256.Sum256([]byte("download.v1:" + c.ID + ":" + c.AssetID + ":" + c.Nonce + ":" + solution))
	return hasLeadingZeros(sum[:], c.Difficulty)
}

func validAltchaSolution(c Challenge, solution string) bool {
	sum := sha256.Sum256([]byte(c.Nonce + ":" + solution))
	return hasLeadingZeros(sum[:], c.Difficulty)
}

func hasLeadingZeros(sum []byte, bits int) bool {
	for _, b := range sum {
		if bits <= 0 {
			return true
		}
		if bits >= 8 {
			if b != 0 {
				return false
			}
			bits -= 8
			continue
		}
		return b>>(8-bits) == 0
	}
	return bits <= 0
}

func randomText(n int) string {
	buf := make([]byte, n)
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

func normalizeSolution(value string) string {
	return strings.TrimSpace(value)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
