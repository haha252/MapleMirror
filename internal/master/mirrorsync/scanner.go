package mirrorsync

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

type Scanner struct {
	Store  Store
	GitHub GitHubClient
	Logger *logging.Logger
}

func (s Scanner) Scan(ctx context.Context, projects config.Projects, projectID, requestID string) (ScanSummary, error) {
	scanID, err := s.Store.StartScan(ctx, projectID, requestID)
	if err != nil {
		return ScanSummary{}, err
	}
	summary := ScanSummary{ScanID: scanID, ProjectID: projectID, RequestID: requestID}
	err = s.scan(ctx, projects, projectID, &summary)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	_ = s.Store.FinishScan(ctx, scanID, summary, errText)
	return summary, err
}

func (s Scanner) scan(ctx context.Context, projects config.Projects, projectID string, summary *ScanSummary) error {
	for _, project := range projects.Projects {
		if !project.Enabled || (projectID != "" && project.ID != projectID) {
			continue
		}
		if s.Logger != nil {
			s.Logger.Debug(ctx, "开始扫描项目",
				slog.String("request_id", summary.RequestID),
				slog.String("project_id", project.ID),
				slog.String("repository", project.Repository))
		}
		releases, err := s.GitHub.ListReleases(ctx, project.Repository)
		if err != nil {
			if s.Logger != nil {
				s.Logger.Warn(ctx, "项目 Release 扫描失败",
					slog.String("request_id", summary.RequestID),
					slog.String("project_id", project.ID),
					slog.String("repository", project.Repository),
					slog.String("error", err.Error()))
			}
			return err
		}
		if s.Logger != nil {
			s.Logger.Debug(ctx, "项目 Release 拉取完成",
				slog.String("request_id", summary.RequestID),
				slog.String("project_id", project.ID),
				slog.String("repository", project.Repository),
				slog.Int("release_count", len(releases)))
		}
		selected := selectReleases(releases, project.IncludePrerelease, project.RetainVersions)
		projectSummary, err := s.writeProject(ctx, project, selected)
		if err != nil {
			return err
		}
		if s.Logger != nil {
			s.Logger.Debug(ctx, "项目扫描写入完成",
				slog.String("request_id", summary.RequestID),
				slog.String("project_id", project.ID),
				slog.Int("selected_releases", projectSummary.SelectedReleases),
				slog.Int("accepted_assets", projectSummary.AcceptedAssets),
				slog.Int("rejected_assets", projectSummary.RejectedAssets))
		}
		summary.SelectedReleases += projectSummary.SelectedReleases
		summary.AcceptedAssets += projectSummary.AcceptedAssets
		summary.RejectedAssets += projectSummary.RejectedAssets
	}
	return nil
}

func (s Scanner) writeProject(ctx context.Context, project config.Project, releases []GitHubRelease) (ScanSummary, error) {
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return ScanSummary{}, err
	}
	defer tx.Rollback()

	now := nowText()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name,
		repository = excluded.repository, enabled = excluded.enabled,
		retain_versions = excluded.retain_versions,
		include_prerelease = excluded.include_prerelease,
		download_multiplier = excluded.download_multiplier,
		config_hash = excluded.config_hash, updated_at = excluded.updated_at`,
		project.ID, project.Name, project.Repository, boolInt(project.Enabled),
		project.RetainVersions, boolInt(project.IncludePrerelease),
		project.DownloadMultiplier, projectHash(project), now); err != nil {
		return ScanSummary{}, err
	}

	summary, err := writeReleases(ctx, tx, project, releases, now, s.Logger)
	if err != nil {
		return ScanSummary{}, err
	}
	if err := rebuildTargetInventory(ctx, tx, project.ID, now); err != nil {
		return ScanSummary{}, err
	}
	generated, err := generateTasks(ctx, tx, now)
	if err != nil {
		return ScanSummary{}, err
	}
	if s.Logger != nil {
		s.Logger.Debug(ctx, "项目目标库存与任务生成完成",
			slog.String("project_id", project.ID),
			slog.Int("selected_releases", summary.SelectedReleases),
			slog.Int("accepted_assets", summary.AcceptedAssets),
			slog.Int("rejected_assets", summary.RejectedAssets),
			slog.Int("generated_tasks", generated))
	}
	return summary, tx.Commit()
}

func writeReleases(ctx context.Context, tx *sql.Tx, project config.Project, releases []GitHubRelease, now string, logger *logging.Logger) (ScanSummary, error) {
	var summary ScanSummary
	_, _ = tx.ExecContext(ctx, `UPDATE releases SET selected = 0 WHERE project_id = ?`, project.ID)
	for _, rel := range releases {
		summary.SelectedReleases++
		releaseID := fmt.Sprintf("%s:%d", project.ID, rel.ID)
		_, err := tx.ExecContext(ctx, `INSERT INTO releases
			(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?)
			ON CONFLICT(project_id, github_release_id) DO UPDATE SET tag_name = excluded.tag_name,
			prerelease = excluded.prerelease, published_at = excluded.published_at, selected = 1`,
			releaseID, project.ID, rel.ID, rel.TagName, boolInt(rel.Prerelease),
			rel.PublishedAt.UTC().Format(time.RFC3339Nano), now)
		if err != nil {
			return summary, err
		}
		accepted, rejected, err := writeAssets(ctx, tx, project, releaseID, rel.Assets, logger, now)
		summary.AcceptedAssets += accepted
		summary.RejectedAssets += rejected
		if err != nil {
			return summary, err
		}
	}
	return summary, nil
}
