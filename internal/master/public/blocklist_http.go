package public

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

func (s Server) rejectBlockedDownload(w http.ResponseWriter, r *http.Request, assetID, stage string) bool {
	clientPrefix := s.clientPrefix(r)
	decision := s.Blocklist.check(clientPrefix)
	if !decision.Blocked && !s.Blocklist.exempt(clientPrefix) && s.Store.DB != nil {
		stored, err := s.Store.ActiveAutoBlock(r.Context(), clientPrefix, time.Now().UTC())
		if err != nil && s.Logger != nil {
			if requestContextDone(err) {
				s.Logger.Debug(r.Context(), "请求已取消，自动封禁状态查询终止",
					slog.String("request_id", requestID(r)),
					slog.String("client_prefix", clientPrefix),
					slog.String("error", err.Error()))
			} else {
				s.Logger.Warn(r.Context(), "自动封禁状态查询失败，继续处理请求",
					slog.String("request_id", requestID(r)),
					slog.String("client_prefix", clientPrefix),
					slog.String("error", err.Error()))
			}
		}
		if err == nil {
			decision = stored
		}
	}
	if !decision.Blocked {
		return false
	}
	if s.Logger != nil {
		s.Logger.Warn(r.Context(), "黑名单客户端被拒绝，封禁后仍尝试下载",
			slog.String("request_id", requestID(r)),
			slog.String("asset_id", assetID),
			slog.String("client_ip", s.clientIP(r)),
			slog.String("client_prefix", clientPrefix),
			slog.String("stage", stage),
			slog.String("block_reason", decision.Reason),
			slog.String("block_source", decision.Source),
			slog.Int64("blocked_after_attempts", decision.Attempts))
	}
	writeError(w, r, http.StatusForbidden, "CLIENT_BLOCKED",
		fmt.Sprintf("您的IP（%s）已被AI封禁！被误封了？请发送此页面截图至frostlynx@qq.com，并说明封禁前你干了什么。", s.clientIP(r)))
	return true
}

func requestContextDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s Server) autoBlockAfterQuotaError(r *http.Request, clientPrefix, assetID string, err error) {
	if s.Blocklist.exempt(clientPrefix) || s.Store.DB == nil {
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
	decision, blockErr := s.Store.AutoBlockClient(r.Context(), clientPrefix, reason,
		"local_auto_ban", time.Now().UTC(), s.Blocklist.autoBanDuration())
	if s.Logger == nil {
		return
	}
	if blockErr != nil {
		s.Logger.Warn(r.Context(), "自动封禁写入失败",
			slog.String("request_id", requestID(r)),
			slog.String("asset_id", assetID),
			slog.String("client_prefix", clientPrefix),
			slog.String("block_reason", reason),
			slog.String("error", blockErr.Error()))
		return
	}
	s.Logger.Warn(r.Context(), "客户端超过额度，已写入自动封禁",
		slog.String("request_id", requestID(r)),
		slog.String("asset_id", assetID),
		slog.String("client_ip", s.clientIP(r)),
		slog.String("client_prefix", clientPrefix),
		slog.String("block_reason", decision.Reason),
		slog.String("block_source", decision.Source),
		slog.Duration("block_duration", s.Blocklist.autoBanDuration()))
}
