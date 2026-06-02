package public

import (
	"database/sql"
	"log/slog"
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
	if loaded.ClientPrefixKey != s.clientPrefix(r) {
		writeError(w, r, http.StatusForbidden, "CLIENT_PREFIX_MISMATCH", "客户端网络前缀不匹配")
		return
	}
	if !s.validSolution(loaded, normalizeSolution(in.Solution)) {
		writeError(w, r, http.StatusForbidden, "CHALLENGE_FAILED", "挑战校验失败")
		return
	}
	auth, debug, err := s.Store.IssueAuthorization(r.Context(), loaded, s.TokenTTL, requestID(r))
	if err != nil {
		code, stable := http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR"
		message := "下载授权签发失败"
		if err == sql.ErrNoRows {
			code, stable = http.StatusConflict, "NO_ROUTABLE_NODE"
			message = "当前没有可用下载节点"
		}
		if err == errRequestQuota {
			code, stable = http.StatusTooManyRequests, "REQUEST_QUOTA_EXHAUSTED"
			message = "请求额度不足，请稍后再试"
		}
		if err == errTrafficLimit {
			code, stable = http.StatusTooManyRequests, "TRAFFIC_LIMIT_EXCEEDED"
			message = "今日流量额度不足，请稍后再试"
		}
		if s.Logger != nil {
			s.Logger.Debug(r.Context(), "下载授权签发失败",
				slog.String("request_id", requestID(r)),
				slog.String("challenge_id", loaded.ID),
				slog.String("asset_id", in.AssetID),
				slog.String("client_prefix", loaded.ClientPrefixKey),
				slog.String("error", err.Error()))
		}
		writeError(w, r, code, stable, message)
		return
	}
	token, err := s.Signer.Sign(auth.Claims)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn(r.Context(), "下载令牌签名失败",
				slog.String("request_id", requestID(r)),
				slog.String("authorization_id", auth.Claims.AuthorizationID),
				slog.String("asset_id", in.AssetID),
				slog.String("client_prefix", loaded.ClientPrefixKey),
				slog.String("error", err.Error()))
		}
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "下载令牌签发失败")
		return
	}
	if s.Logger != nil {
		s.Logger.Info(r.Context(), "下载令牌已签发",
			slog.String("request_id", requestID(r)),
			slog.String("authorization_id", auth.Claims.AuthorizationID),
			slog.String("asset_id", in.AssetID),
			slog.String("client_ip", s.clientIP(r)),
			slog.String("node_id", debug.NodeID),
			slog.String("node_name", debug.NodeName),
			slog.String("project_id", debug.ProjectID),
			slog.String("system", debug.System),
			slog.String("architecture", debug.Architecture),
			slog.String("client_prefix", debug.ClientPrefix),
			slog.String("expires_at", debug.ExpiresAt),
			slog.Int64("max_bytes", debug.MaxBytes),
			slog.Int("range_limit", debug.RangeLimit),
			slog.Any("request_remaining_tokens", remainingTokens(debug.RequestRemainingMicrounits)),
			slog.Any("request_remaining_microunits", debug.RequestRemainingMicrounits),
			slog.Any("traffic_remaining_bytes", debug.TrafficRemainingBytes))
	}
	writeOK(w, r, http.StatusCreated, "下载授权已签发", map[string]any{
		"authorization_id":        auth.Claims.AuthorizationID,
		"download_url":            debug.DownloadURL,
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

func remainingTokens(microunits map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(microunits))
	for scope, value := range microunits {
		out[scope] = value / tokenUnit
	}
	return out
}

func (s Server) authorization(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/api/public/v1/authorizations/"):]
	auth, err := s.Store.Authorization(r.Context(), id)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "ASSET_NOT_FOUND", "授权不存在")
		return
	}
	sent, first, _ := s.Store.AuthorizationBytes(r.Context(), id)
	writeOK(w, r, http.StatusOK, "查询成功", map[string]any{
		"authorization_id": auth.AuthorizationID, "asset_id": auth.AssetID,
		"node_id": auth.NodeID, "state": auth.State, "expires_at": auth.ExpiresAt,
		"bytes_accounting_enabled": true, "sent_bytes": sent, "first_transfer_at": first,
	})
}

func expiresAfter(ttl time.Duration) string {
	return time.Now().UTC().Add(ttl).Format(time.RFC3339Nano)
}
