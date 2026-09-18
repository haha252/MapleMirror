package mirrorsync

import (
	"context"
	"log/slog"
	"os"
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

type projectFileState struct {
	ModTime time.Time
	Size    int64
	Valid   bool
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
	return s.trigger(ctx, projectID, requestID, false)
}

func (s Service) trigger(ctx context.Context, projectID, requestID string, resetVersionBaseline bool) (string, error) {
	if requestID == "" {
		requestID, _ = requestid.New()
	}
	if s.Logger != nil {
		message := "手动触发 Release 扫描"
		if resetVersionBaseline {
			message = "手动重置版本状态并触发 Release 扫描"
		}
		s.Logger.Debug(ctx, message,
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
	if err := s.syncProjectConfig(ctx, projects); err != nil {
		return "", err
	}
	if projectID == "" {
		return s.triggerAll(ctx, projects, requestID)
	}
	summary, err := s.scanProject(ctx, projects, projectID, requestID, resetVersionBaseline)
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

func (s Service) scheduleNextScan(ctx context.Context, projectID string, scanErr error) {
	if projectID == "" || s.Interval <= 0 {
		return
	}
	delay := s.Interval + stableProjectJitter(projectID, s.Interval)
	if scanErr != nil {
		delay = time.Minute
		if s.Interval < delay {
			delay = s.Interval
		}
	}
	next := time.Now().UTC().Add(delay).Format(time.RFC3339Nano)
	stateCtx, cancel := scanStateContext(ctx)
	defer cancel()
	if err := s.Scanner.Store.SetProjectNextScan(stateCtx, projectID, next); err != nil && s.Logger != nil {
		s.Logger.Warn(ctx, "更新项目下次扫描时间失败",
			slog.String("project_id", projectID),
			slog.String("next_scan_at", next),
			slog.String("error", err.Error()))
	}
}

func (s Service) Run(ctx context.Context) {
	if s.Interval <= 0 {
		return
	}
	if s.Logger != nil {
		s.Logger.Debug(ctx, "Release 扫描调度器启动",
			slog.String("project_interval", s.Interval.String()))
	}
	lastProjectState := s.projectFileState()
	s.runDue(ctx)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nextProjectState := s.projectFileState()
			if projectFileChanged(lastProjectState, nextProjectState) {
				if s.Logger != nil {
					s.Logger.Info(ctx, "检测到项目清单变更，立即触发 Release 扫描")
				}
				s.reloadProjects(ctx)
			}
			lastProjectState = nextProjectState
			s.runDue(ctx)
		}
	}
}

func (s Service) runDue(ctx context.Context) {
	if err := s.reloadProjects(ctx); err != nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	ids, err := s.Scanner.Store.DueProjects(ctx, now)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn(ctx, "查询到期项目扫描失败", slog.String("error", err.Error()))
		}
		return
	}
	for _, projectID := range ids {
		s.runOnce(ctx, projectID)
	}
}

func (s Service) runOnce(ctx context.Context, projectID string) {
	reqID, _ := requestid.New()
	if _, err := s.Trigger(ctx, projectID, reqID); err != nil && s.Logger != nil {
		s.Logger.Warn(ctx, "Release 扫描失败", slog.String("request_id", reqID),
			slog.String("error", err.Error()))
	}
}

func (s Service) reloadProjects(ctx context.Context) error {
	if s.Projects == nil {
		return nil
	}
	projects, err := s.Projects.Load()
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn(ctx, "项目清单热重载失败，沿用上一次有效配置",
				slog.String("error", err.Error()))
		}
		projects = s.Projects.Current()
	}
	if syncErr := s.syncProjectConfig(ctx, projects); syncErr != nil {
		if s.Logger != nil {
			s.Logger.Warn(ctx, "项目扫描状态同步失败",
				slog.String("error", syncErr.Error()))
		}
		return syncErr
	}
	return nil
}

func (s Service) syncProjectConfig(ctx context.Context, projects config.Projects) error {
	return s.Scanner.Store.SyncProjectConfig(ctx, projects)
}

func (s Service) projectFileState() projectFileState {
	if s.Projects == nil || s.Projects.Path == "" {
		return projectFileState{}
	}
	info, err := os.Stat(s.Projects.Path)
	if err != nil {
		return projectFileState{}
	}
	return projectFileState{ModTime: info.ModTime(), Size: info.Size(), Valid: true}
}

func projectFileChanged(previous, next projectFileState) bool {
	if !previous.Valid || !next.Valid {
		return previous.Valid != next.Valid
	}
	return !previous.ModTime.Equal(next.ModTime) || previous.Size != next.Size
}
