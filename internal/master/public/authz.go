package public

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"
)

type challengeSubmit struct {
	Kind        string
	ChallengeID string
	AssetID     string
	Solution    string
}

func (s Server) authorize(w http.ResponseWriter, r *http.Request, in challengeSubmit) {
	loaded, err := s.Store.LoadChallenge(r.Context(), in.ChallengeID)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "CHALLENGE_REQUIRED", "挑战不存在或已失效")
		return
	}
	if loaded.Kind != in.Kind || loaded.AssetID != in.AssetID {
		writeError(w, r, http.StatusForbidden, "CHALLENGE_FAILED", "挑战与资产不匹配")
		return
	}
	if loaded.ClientPrefixKey != clientPrefix(r) {
		writeError(w, r, http.StatusForbidden, "CLIENT_PREFIX_MISMATCH", "客户端网络前缀不匹配")
		return
	}
	if !s.validSolution(loaded, normalizeSolution(in.Solution)) {
		writeError(w, r, http.StatusForbidden, "CHALLENGE_FAILED", "挑战校验失败")
		return
	}
	auth, err := s.Store.IssueAuthorization(r.Context(), loaded, s.TokenTTL, requestID(r))
	if err != nil {
		code, stable := http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR"
		if err == sql.ErrNoRows {
			code, stable = http.StatusConflict, "NO_ROUTABLE_NODE"
		}
		writeError(w, r, code, stable, "当前没有可用下载节点")
		return
	}
	token, err := s.Signer.Sign(auth.Claims)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "下载令牌签发失败")
		return
	}
	writeOK(w, r, http.StatusCreated, "下载授权已签发", map[string]any{
		"authorization_id":        auth.Claims.AuthorizationID,
		"download_url":            "/downloads/" + auth.Claims.AssetID,
		"download_token":          token,
		"expires_at":              auth.Claims.ExpiresAt,
		"range_concurrency_limit": auth.Claims.RangeConcurrencyLimit,
		"max_bytes":               auth.Claims.MaxBytes,
	})
}

func (s Server) validSolution(c Challenge, solution string) bool {
	if c.Kind == "api_pow" {
		return validLeadingZeros(c, solution)
	}
	n, err := strconv.Atoi(solution)
	return err == nil && n >= 0 && validAltchaSolution(c, solution)
}

func (s Server) authorization(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/api/public/v1/authorizations/"):]
	auth, err := s.Store.Authorization(r.Context(), id)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "ASSET_NOT_FOUND", "授权不存在")
		return
	}
	writeOK(w, r, http.StatusOK, "查询成功", map[string]any{
		"authorization_id": auth.AuthorizationID, "asset_id": auth.AssetID,
		"node_id": auth.NodeID, "state": auth.State, "expires_at": auth.ExpiresAt,
		"bytes_accounting_enabled": false, "sent_bytes": nil,
	})
}

func expiresAfter(ttl time.Duration) string {
	return time.Now().UTC().Add(ttl).Format(time.RFC3339Nano)
}
