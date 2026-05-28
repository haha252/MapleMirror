package mirrorsync

import (
	"context"
	"log/slog"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/requestid"
)

type Service struct {
	Scanner  Scanner
	Projects config.Projects
	Interval time.Duration
	Logger   *logging.Logger
}

func (s Service) Trigger(ctx context.Context, projectID, requestID string) (string, error) {
	if requestID == "" {
		requestID, _ = requestid.New()
	}
	summary, err := s.Scanner.Scan(ctx, s.Projects, projectID, requestID)
	return summary.ScanID, err
}

func (s Service) Run(ctx context.Context) {
	if s.Interval <= 0 {
		return
	}
	s.runOnce(ctx, "")
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx, "")
		}
	}
}

func (s Service) runOnce(ctx context.Context, projectID string) {
	reqID, _ := requestid.New()
	if _, err := s.Trigger(ctx, projectID, reqID); err != nil && s.Logger != nil {
		s.Logger.Warn(ctx, "Release 扫描失败", slog.String("request_id", reqID),
			slog.String("error", err.Error()))
	}
}
