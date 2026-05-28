package public

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type altchaPayload struct {
	Challenge string `json:"challenge"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
	Number    int    `json:"number"`
}

func (s Server) webChallenge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AssetID string `json:"asset_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	challenge, err := s.Store.CreateChallenge(r.Context(), "altcha", in.AssetID,
		clientPrefix(r), 10, s.ALTCHATTL, requestID(r))
	if err != nil {
		writeError(w, r, http.StatusConflict, "NO_ROUTABLE_NODE", "当前没有可用下载节点")
		return
	}
	payload := s.altchaPayload(challenge)
	writeOK(w, r, http.StatusCreated, "挑战已创建", map[string]any{
		"challenge_id": challenge.ID, "altcha": payload, "expires_at": challenge.ExpiresAt,
	})
}

func (s Server) apiChallenge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AssetID string `json:"asset_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	challenge, err := s.Store.CreateChallenge(r.Context(), "api_pow", in.AssetID,
		clientPrefix(r), s.APIZeroBits, s.APITTL, requestID(r))
	if err != nil {
		writeError(w, r, http.StatusConflict, "NO_ROUTABLE_NODE", "当前没有可用下载节点")
		return
	}
	writeOK(w, r, http.StatusCreated, "挑战已创建", map[string]any{
		"challenge_id": challenge.ID, "asset_id": in.AssetID, "nonce_seed": challenge.Nonce,
		"algorithm": "sha256", "leading_zero_bits": s.APIZeroBits,
		"expires_at":       challenge.ExpiresAt,
		"canonical_format": "download.v1:{challenge_id}:{asset_id}:{nonce_seed}:{nonce}",
	})
}

func (s Server) webAuthorize(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "ALTCHA 提交内容不合法")
		return
	}
	s.authorize(w, r, challengeSubmit{Kind: "altcha", ChallengeID: in.ChallengeID,
		AssetID: in.AssetID, Solution: strconv.Itoa(payload.Number)})
}

func (s Server) apiAuthorize(w http.ResponseWriter, r *http.Request) {
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
	mac := hmac.New(sha256.New, s.Signer.key)
	_, _ = mac.Write([]byte(c.Nonce + ":" + salt))
	return altchaPayload{Challenge: c.Nonce, Salt: salt, Signature: base64.RawURLEncoding.EncodeToString(mac.Sum(nil))}
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
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不合法")
		return false
	}
	return true
}

func normalizeSolution(value string) string {
	return strings.TrimSpace(value)
}
