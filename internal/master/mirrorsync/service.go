package mirrorsync

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/requestid"
)

type Service struct {
	Scanner  Scanner
	Projects *ProjectLoader
	Interval time.Duration
	Logger   *logging.Logger
}

type ProjectLoader struct {
	Path     string
	Fallback config.Projects
	mu       sync.RWMutex
}

func NewProjectLoader(path string, initial config.Projects) *ProjectLoader {
	return &ProjectLoader{Path: path, Fallback: initial}
}

func (l *ProjectLoader) Load() (config.Projects, error) {
	if l == nil || l.Path == "" {
		return l.Current(), nil
	}
	projects, err := config.LoadProjects(l.Path, nil)
	if err != nil {
		return l.Current(), err
	}
	l.mu.Lock()
	l.Fallback = projects
	l.mu.Unlock()
	return projects, nil
}

func (l *ProjectLoader) Current() config.Projects {
	if l == nil {
		return config.Projects{}
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.Fallback
}

func (s Service) Trigger(ctx context.Context, projectID, requestID string) (string, error) {
	if requestID == "" {
		requestID, _ = requestid.New()
	}
	if s.Logger != nil {
		s.Logger.Debug(ctx, "手动触发 Release 扫描",
			slog.String("request_id", requestID),
			slog.String("project_id", projectID))
	}
	projects := s.Projects.Current()
	var loadErr error
	if s.Projects != nil {
		projects, loadErr = s.Projects.Load()
	}
	if loadErr != nil && s.Logger != nil {
		s.Logger.Warn(ctx, "项目清单热重载失败，沿用上一次有效配置",
			slog.String("request_id", requestID),
			slog.String("error", loadErr.Error()))
	}
	summary, err := s.Scanner.Scan(ctx, projects, projectID, requestID)
	if s.Logger != nil {
		fields := []slog.Attr{
			slog.String("request_id", requestID),
			slog.String("scan_id", summary.ScanID),
			slog.String("project_id", summary.ProjectID),
			slog.Int("selected_releases", summary.SelectedReleases),
			slog.Int("accepted_assets", summary.AcceptedAssets),
			slog.Int("rejected_assets", summary.RejectedAssets),
		}
		if err != nil {
			fields = append(fields, slog.String("error", err.Error()))
			s.Logger.Warn(ctx, "Release 扫描结束", fields...)
		} else {
			s.Logger.Debug(ctx, "Release 扫描结束", fields...)
		}
	}
	return summary.ScanID, err
}

func (s Service) Run(ctx context.Context) {
	if s.Interval <= 0 {
		return
	}
	if s.Logger != nil {
		s.Logger.Debug(ctx, "Release 扫描调度器启动", slog.String("interval", s.Interval.String()))
	}
	s.runOnce(ctx, "")
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.Logger != nil {
				s.Logger.Debug(ctx, "Release 扫描调度 tick", slog.String("interval", s.Interval.String()))
			}
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
