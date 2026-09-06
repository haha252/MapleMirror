package public

import (
	"crypto/sha256"
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"mirror-server/internal/downloadtoken"
)

type challengeSubmit struct {
	SourceKind      string
	ProtocolVersion string
	Algorithm       string
	ChallengeID     string
	AssetID         string
	Solution        string
	Telemetry       *powTelemetryInput
}

func (s Server) authorize(w http.ResponseWriter, r *http.Request, in challengeSubmit) {
	prefix := s.clientPrefix(r)
	sourceKind := in.SourceKind
	abuseKind := abuseScope(sourceKind, in.ProtocolVersion)
	now := time.Now().UTC()
	loaded, err := s.Store.LoadChallenge(r.Context(), in.ChallengeID)
	if err != nil {
		if s.AbuseTracker != nil && s.AbuseTracker.mode != "off" {
			s.AbuseTracker.record(abuseKind, prefix, s.invalidSolutionWeight(), now)
		}
		writeError(w, r, http.StatusNotFound, "CHALLENGE_REQUIRED", "挑战不存在或已失效")
		return
	}
	if loaded.SourceKind != in.SourceKind || loaded.ProtocolVersion != in.ProtocolVersion ||
		loaded.Algorithm != in.Algorithm || loaded.AssetID != in.AssetID {
		if s.AbuseTracker != nil && s.AbuseTracker.mode != "off" {
			s.AbuseTracker.record(abuseKind, prefix, s.invalidSolutionWeight(), now)
		}
		writeError(w, r, http.StatusForbidden, "CHALLENGE_FAILED", "挑战与资产不匹配")
		return
	}
	if loaded.ClientPrefixKey != prefix {
		if s.AbuseTracker != nil && s.AbuseTracker.mode != "off" {
			s.AbuseTracker.record(abuseKind, prefix, s.invalidSolutionWeight(), now)
		}
		writeError(w, r, http.StatusForbidden, "CHALLENGE_FAILED", "挑战与客户端不匹配")
		return
	}
	solution := in.Solution
	if loaded.ProtocolVersion == "v1" {
		solution = normalizeSolution(solution)
	}
	if !s.validSolution(loaded, solution) {
		if s.AbuseTracker != nil && s.AbuseTracker.mode != "off" {
			s.AbuseTracker.record(abuseKind, prefix, s.invalidSolutionWeight(), now)
		}
		writeError(w, r, http.StatusForbidden, "CHALLENGE_FAILED", "挑战校验失败")
		return
	}
	auth, debug, token, err := s.Store.IssueSignedAuthorization(r.Context(),
		loaded, s.TokenLifetime, requestID(r), func(downloadtoken.Claims) (string, error) {
			return downloadtoken.NewOpaque()
		})
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
			s.autoBlockAfterQuotaError(r, loaded.ClientPrefixKey, in.AssetID, err)
		}
		if err == errTrafficLimit {
			code, stable = http.StatusTooManyRequests, "TRAFFIC_LIMIT_EXCEEDED"
			message = "今日流量额度不足，请稍后再试"
			s.autoBlockAfterQuotaError(r, loaded.ClientPrefixKey, in.AssetID, err)
		}
		if err == errChallengeBusy {
			code, stable = http.StatusConflict, "CHALLENGE_IN_PROGRESS"
			message = "挑战正在处理，请稍后重试"
		}
		if s.Logger != nil {
			s.Logger.Debug(r.Context(), "下载授权签发失败",
				slog.String("request_id", requestID(r)),
				slog.String("challenge_id", loaded.ID),
				slog.String("asset_id", in.AssetID),
				slog.String("client_source", fullPublicSource(loaded.ClientPrefixKey)),
				slog.String("error", err.Error()))
		}
		writeError(w, r, code, stable, message)
		return
	}
	if err := s.Store.waitForAuthorizationDelivered(r.Context(),
		auth.Claims.AuthorizationID, debug.NodeID); err != nil {
		if s.Logger != nil {
			s.Logger.Debug(r.Context(), "等待下载授权同步到节点失败",
				slog.String("request_id", requestID(r)),
				slog.String("authorization_id", auth.Claims.AuthorizationID),
				slog.String("asset_id", in.AssetID),
				slog.String("node_id", debug.NodeID),
				slog.String("error", err.Error()))
		}
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "下载授权同步到节点失败")
		return
	}
	if expiresAt, err := s.Store.authorizationExpiresAt(r.Context(),
		auth.Claims.AuthorizationID, debug.NodeID); err != nil {
		if s.Logger != nil {
			s.Logger.Debug(r.Context(), "读取下载授权过期时间失败",
				slog.String("request_id", requestID(r)),
				slog.String("authorization_id", auth.Claims.AuthorizationID),
				slog.String("node_id", debug.NodeID),
				slog.String("error", err.Error()))
		}
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "下载授权签发失败")
		return
	} else if expiresAt != "" {
		auth.Claims.ExpiresAt = expiresAt
		debug.ExpiresAt = expiresAt
	}
	if s.Logger != nil {
		attrs := []slog.Attr{
			slog.String("request_id", requestID(r)),
			slog.String("authorization_id", auth.Claims.AuthorizationID),
			slog.String("asset_id", in.AssetID),
			slog.String("client_source", fullPublicSource(s.clientIP(r))),
			slog.String("node_id", debug.NodeID),
			slog.String("node_name", debug.NodeName),
			slog.String("project_id", debug.ProjectID),
			slog.String("system", debug.System),
			slog.String("architecture", debug.Architecture),
			slog.String("pow_algorithm", loaded.Algorithm),
			slog.String("pow_protocol_version", loaded.ProtocolVersion),
			slog.Int64("challenge_age_ms", challengeAgeMilliseconds(loaded, time.Now().UTC())),
			slog.String("expires_at", debug.ExpiresAt),
			slog.Int64("max_bytes", debug.MaxBytes),
			slog.Int("range_limit", debug.RangeLimit),
			slog.Any("request_remaining_tokens", remainingTokens(debug.RequestRemainingMicrounits)),
			slog.Any("request_remaining_microunits", debug.RequestRemainingMicrounits),
			slog.Any("traffic_remaining_bytes", debug.TrafficRemainingBytes),
		}
		if loaded.ProtocolVersion == "v1" {
			attrs = append(attrs, slog.Int("pow_difficulty", loaded.Difficulty))
		} else {
			attrs = append(attrs, slog.Uint64("pow_iterations", loaded.Iterations),
				slog.Int("pow_multiplier", loaded.Multiplier), slog.String("modulus_id", loaded.ModulusID))
		}
		s.Logger.Info(r.Context(), "下载令牌已签发", attrs...)
	}
	s.writePoWTelemetry(r, loaded, auth.Claims.AuthorizationID, in.Telemetry)
	writeOK(w, r, http.StatusCreated, "下载授权已签发", map[string]any{
		"authorization_id":        auth.Claims.AuthorizationID,
		"download_url":            debug.DownloadURL,
		"download_token":          token,
		"expires_at":              auth.Claims.ExpiresAt,
		"range_concurrency_limit": auth.Claims.RangeConcurrencyLimit,
		"max_bytes":               auth.Claims.MaxBytes,
	})
}

func (s Server) invalidSolutionWeight() int64 {
	if s.AbuseTracker == nil || s.AbuseTracker.cfg.Challenge.InvalidSolutionWeight <= 0 {
		return 4
	}
	return int64(s.AbuseTracker.cfg.Challenge.InvalidSolutionWeight)
}

func (s Server) validSolution(c Challenge, solution string) bool {
	if c.ProtocolVersion == "v2" && c.Algorithm == vdfAlgorithm {
		return validVDFSolution(c, solution)
	}
	if c.ProtocolVersion == "v1" && c.Algorithm == "sha256" {
		return validLeadingZeros(c, solution)
	}
	if c.ProtocolVersion == "v1" && c.Algorithm == "altcha-sha256-v1" {
		n, err := strconv.Atoi(solution)
		return err == nil && n >= 0 && validAltchaSolution(c, solution)
	}
	return false
}

func validAltchaSolution(c Challenge, solution string) bool {
	sum := sha256.Sum256([]byte(c.Nonce + ":" + solution))
	return hasLeadingZeros(sum[:], c.Difficulty)
}

func challengeAgeMilliseconds(c Challenge, now time.Time) int64 {
	created, err := time.Parse(time.RFC3339Nano, c.CreatedAt)
	if err != nil || now.Before(created) {
		return 0
	}
	return now.Sub(created).Milliseconds()
}

func remainingTokens(microunits map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(microunits))
	for scope, value := range microunits {
		out[scope] = value / tokenUnit
	}
	return out
}

func expiresAfter(now time.Time, ttl time.Duration) string {
	return now.Add(ttl).Format(time.RFC3339Nano)
}
