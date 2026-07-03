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
	Source ResourceSource
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
	stateCtx, cancel := scanStateContext(ctx)
	defer cancel()
	_ = s.Store.FinishScan(stateCtx, scanID, summary, errText)
	return summary, err
}

func (s Scanner) scan(ctx context.Context, projects config.Projects, projectID string, summary *ScanSummary) error {
	if err := s.Store.SyncProjectConfig(ctx, projects); err != nil {
		return err
	}
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
		source := s.Source
		if source == nil {
			source = GitHubReleaseSource{Client: s.GitHub}
		}
		releases, err := source.ListResourceVersions(ctx, project)
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
		previous, err := s.Store.ReleaseRegression(ctx, project.ID, selected)
		if err != nil {
			return err
		}
		if previous != nil {
			err = s.confirmReleaseRegression(ctx, project, *previous)
		}
		if err != nil {
			if s.Logger != nil {
				s.Logger.Warn(ctx, "拒绝回退到更旧的 Release 快照",
					slog.String("request_id", summary.RequestID),
					slog.String("project_id", project.ID),
					slog.String("error", err.Error()))
			}
			return err
		}
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

func (s Scanner) confirmReleaseRegression(ctx context.Context, project config.Project, previous ResourceVersion) error {
	verifier, ok := s.GitHub.(GitHubReleaseVerifier)
	if !ok {
		return fmt.Errorf("新快照早于已选 Release %q，且当前来源无法确认该 Release 是否已删除", previous.Version)
	}
	exists, err := verifier.ReleaseExists(ctx, project.Repository, previous.NumericID)
	if err != nil {
		return fmt.Errorf("新快照早于已选 Release %q，二次确认失败：%w", previous.Version, err)
	}
	if exists {
		return fmt.Errorf("新快照早于仍存在的已选 Release %q，判定为陈旧快照", previous.Version)
	}
	if s.Logger != nil {
		s.Logger.Info(ctx, "GitHub 已删除之前选中的 Release，允许版本回退",
			slog.String("project_id", project.ID),
			slog.String("release", previous.Version),
			slog.Int64("github_release_id", previous.NumericID))
	}
	return nil
}

func (s Scanner) writeProject(ctx context.Context, project config.Project, releases []ResourceVersion) (ScanSummary, error) {
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return ScanSummary{}, err
	}
	defer tx.Rollback()

	now := nowText()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects
		(id, name, repository, description, homepage_url, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name,
		repository = excluded.repository,
		description = excluded.description,
		homepage_url = excluded.homepage_url,
		enabled = excluded.enabled,
		retain_versions = excluded.retain_versions,
		include_prerelease = excluded.include_prerelease,
		download_multiplier = excluded.download_multiplier,
		config_hash = excluded.config_hash, updated_at = excluded.updated_at`,
		project.ID, project.Name, project.Repository, project.Description,
		project.HomepageURL, boolInt(project.Enabled),
		project.RetainVersions, boolInt(project.IncludePrerelease),
		project.DownloadMultiplier, projectHash(project), now); err != nil {
		return ScanSummary{}, err
	}

	summary, err := writeReleases(ctx, tx, project, releases, now, s.Logger)
	if err != nil {
		return ScanSummary{}, err
	}
	if err := supersedeDuplicatePublicPaths(ctx, tx, project.ID); err != nil {
		return ScanSummary{}, err
	}
	if err := rebuildTargetInventory(ctx, tx, project.ID, now); err != nil {
		return ScanSummary{}, err
	}
	if err := cancelObsoleteDownloadTasks(ctx, tx, project.ID, now); err != nil {
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
	if err := tx.Commit(); err != nil {
		return ScanSummary{}, err
	}
	if generated > 0 {
		s.Store.NotifyProjectTaskNodes(ctx, project.ID)
	}
	return summary, nil
}

func writeReleases(ctx context.Context, tx *sql.Tx, project config.Project, releases []ResourceVersion, now string, logger *logging.Logger) (ScanSummary, error) {
	var summary ScanSummary
	_, _ = tx.ExecContext(ctx, `UPDATE releases SET selected = 0 WHERE project_id = ?`, project.ID)
	for _, rel := range releases {
		summary.SelectedReleases++
		releaseID := fmt.Sprintf("%s:%d", project.ID, rel.NumericID)
		_, err := tx.ExecContext(ctx, `INSERT INTO releases
			(id, project_id, github_release_id, tag_name, prerelease, published_at,
			selected, created_at, source_type, source_release_key)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?)
			ON CONFLICT(project_id, github_release_id) DO UPDATE SET tag_name = excluded.tag_name,
			prerelease = excluded.prerelease, published_at = excluded.published_at,
			selected = 1, source_type = excluded.source_type,
			source_release_key = excluded.source_release_key`,
			releaseID, project.ID, rel.NumericID, rel.Version, boolInt(rel.Prerelease),
			rel.PublishedAt.UTC().Format(time.RFC3339Nano), now,
			rel.SourceType, rel.SourceReleaseKey)
		if err != nil {
			return summary, err
		}
		accepted, rejected, err := writeAssets(ctx, tx, project, releaseID, rel, logger, now)
		summary.AcceptedAssets += accepted
		summary.RejectedAssets += rejected
		if err != nil {
			return summary, err
		}
	}
	return summary, nil
}
