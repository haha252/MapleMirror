package public

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (s Server) rejectBlockedDownload(w http.ResponseWriter, r *http.Request, assetID, stage string) bool {
	clientPrefix := s.clientPrefix(r)
	decision := blockDecision{}
	if s.Blocklist != nil {
		decision = s.Blocklist.check(clientPrefix)
	}
	now := time.Now().UTC()
	var stored clientBlockDecision
	exempt := s.Blocklist != nil && s.Blocklist.exempt(clientPrefix)
	if !decision.Blocked && s.ClientBlocks != nil && !exempt {
		var err error
		stored, err = s.ClientBlocks.resolve(r.Context(), clientPrefix, now)
		if err != nil && s.Logger != nil {
			if requestContextDone(err) {
				s.Logger.Debug(r.Context(), "请求已取消，客户端封禁查询终止",
					slog.String("request_id", requestID(r)),
					slog.String("client_source", fullPublicSource(clientPrefix)),
					slog.String("error", err.Error()))
			} else {
				s.Logger.Warn(r.Context(), "客户端封禁状态查询失败，继续处理请求",
					slog.String("request_id", requestID(r)),
					slog.String("client_source", fullPublicSource(clientPrefix)),
					slog.String("error", err.Error()))
			}
		}
		decision = convertClientBlockDecision(stored)
	}
	if !decision.Blocked && s.ClientBlocks == nil && !exempt && s.Store.DB != nil {
		fallback, err := s.Store.ActiveAutoBlock(r.Context(), clientPrefix, now)
		if err != nil && s.Logger != nil {
			if requestContextDone(err) {
				s.Logger.Debug(r.Context(), "请求已取消，自动封禁状态查询终止",
					slog.String("request_id", requestID(r)),
					slog.String("client_source", fullPublicSource(clientPrefix)),
					slog.String("error", err.Error()))
			} else {
				s.Logger.Warn(r.Context(), "自动封禁状态查询失败，继续处理请求",
					slog.String("request_id", requestID(r)),
					slog.String("client_source", fullPublicSource(clientPrefix)),
					slog.String("error", err.Error()))
			}
		}
		if fallback.Blocked {
			decision = fallback
		}
	}
	if !decision.Blocked {
		return false
	}
	if s.ClientBlocks != nil && stored.Blocked {
		s.ClientBlocks.recordAttempt(r.Context(), stored, now)
		if updated, err := s.ClientBlocks.resolve(r.Context(), clientPrefix, now); err == nil && updated.Blocked {
			decision = convertClientBlockDecision(updated)
			decision.Attempts = s.ClientBlocks.estimatedAttempts(updated.Key, updated.Attempts)
		}
	}
	if s.Logger != nil && shouldLogAttempt(decision.Attempts) {
		s.Logger.Warn(r.Context(), "黑名单客户端被拒绝，封禁后仍尝试下载",
			slog.String("request_id", requestID(r)),
			slog.String("asset_id", assetID),
			slog.String("client_source", fullPublicSource(s.clientIP(r))),
			slog.String("stage", stage),
			slog.String("block_reason", decision.Reason),
			slog.String("block_source", decision.Source),
			slog.Int("escalation_level", decision.EscalationLevel),
			slog.Bool("punishment_active", decision.PunishmentActive),
			slog.Int64("blocked_after_attempts", decision.Attempts))
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusForbidden, response{
			Status:    "error",
			Code:      "CLIENT_BLOCKED",
			Message:   "当前来源已被限制访问",
			RequestID: requestID(r),
			Data: map[string]string{
				"source": fullPublicSource(s.clientIP(r)),
			},
		})
		return true
	}
	if decision.PunishmentActive && s.punishmentEnabled() {
		s.renderPunishmentPage(w, r, decision)
		return true
	}
	s.renderBlockedPage(w, r, decision)
	return true
}

func requestContextDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s Server) autoBlockAfterQuotaError(r *http.Request, clientPrefix, assetID string, err error) {
	if (s.Blocklist != nil && s.Blocklist.exempt(clientPrefix)) || s.Store.DB == nil {
		return
	}
	reason := ""
	switch err {
	case errRequestQuota:
		reason = "request_quota_exhausted"
	case errTrafficLimit:
		reason = "traffic_limit_exceeded"
	default:
		return
	}
	blockErr := s.Store.upsertAutoBlock(r.Context(), clientPrefix, reason,
		"local_auto_ban", time.Now().UTC(), s.blocklistAutoBanDuration())
	if blockErr != nil {
		if s.Logger != nil {
			s.Logger.Warn(r.Context(), "自动封禁写入失败",
				slog.String("request_id", requestID(r)),
				slog.String("asset_id", assetID),
				slog.String("client_source", fullPublicSource(clientPrefix)),
				slog.String("block_reason", reason),
				slog.String("error", blockErr.Error()))
		}
		return
	}
	if s.ClientBlocks != nil {
		s.ClientBlocks.invalidate(clientPrefix)
	}
	if s.Logger != nil {
		s.Logger.Warn(r.Context(), "客户端超过额度，已写入自动封禁",
			slog.String("request_id", requestID(r)),
			slog.String("asset_id", assetID),
			slog.String("client_source", fullPublicSource(s.clientIP(r))),
			slog.String("block_reason", reason),
			slog.String("block_source", "local_auto_ban"),
			slog.Duration("block_duration", s.blocklistAutoBanDuration()))
	}
}

func convertClientBlockDecision(in clientBlockDecision) blockDecision {
	if !in.Blocked {
		return blockDecision{}
	}
	return blockDecision{
		Blocked:          true,
		Reason:           in.Reason,
		Source:           in.Source,
		Attempts:         in.Attempts,
		Key:              in.Key,
		BlockedAt:        in.BlockedAt,
		ExpiresAt:        in.ExpiresAt,
		EscalationLevel:  in.EscalationLevel,
		PunishmentActive: in.PunishmentActive,
	}
}

func (s Server) punishmentEnabled() bool {
	return s.AbuseTracker != nil && s.AbuseTracker.cfg.Punishment.Enabled != nil && *s.AbuseTracker.cfg.Punishment.Enabled
}

func (s Server) blocklistAutoBanDuration() time.Duration {
	if s.Blocklist == nil {
		return 7 * 24 * time.Hour
	}
	return s.Blocklist.autoBanDuration()
}
