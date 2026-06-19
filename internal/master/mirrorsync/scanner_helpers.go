package mirrorsync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/assetstate"
)

func writeAssets(ctx context.Context, tx *sql.Tx, project config.Project, releaseID string, release ResourceVersion, logger *logging.Logger, now string) (int, int, error) {
	classifier, err := newAssetClassifier(project)
	if err != nil {
		return 0, 0, err
	}
	acceptedIDs := map[int64]struct{}{}
	var accepted, rejected int
	assets := release.Assets
	for _, asset := range assets {
		allowed, reason, err := assetAllowed(asset.FileName, project.AssetInclude, project.AssetExclude)
		if err != nil {
			return accepted, rejected, err
		}
		if !allowed {
			if logger != nil {
				logger.Debug(ctx, "资产未进入镜像流程",
					slog.String("project_id", project.ID),
					slog.String("release_id", releaseID),
					slog.String("asset_name", asset.FileName),
					slog.String("reason", reason))
			}
			rejected++
			continue
		}
		digest, err := normalizeDigest(asset.Digest)
		if err != nil {
			if logger != nil {
				logger.Debug(ctx, "资产未进入镜像流程",
					slog.String("project_id", project.ID),
					slog.String("release_id", releaseID),
					slog.String("asset_name", asset.FileName),
					slog.String("reason", "digest invalid"))
			}
			rejected++
			continue
		}
		classification, err := classifier.Classify(asset, release, assets)
		if err != nil {
			return accepted, rejected, err
		}
		labelsJSON, err := classificationLabelsJSON(classification.Labels)
		if err != nil {
			return accepted, rejected, err
		}
		assetID := fmt.Sprintf("%s:%d", releaseID, asset.NumericID)
		if err := markInventoryStaleOnAssetChange(ctx, tx, assetID, digest, asset.SizeBytes, now); err != nil {
			return accepted, rejected, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO assets
			(id, release_id, github_asset_id, file_name, architecture, system, size_bytes,
			source_url, digest_sha256, service_state, created_at, source_type,
			source_asset_key, variant, display_label, priority, labels_json,
			classification_reason)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'candidate', ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(release_id, github_asset_id) DO UPDATE SET
			file_name = excluded.file_name, architecture = excluded.architecture,
			system = excluded.system,
			size_bytes = excluded.size_bytes, source_url = excluded.source_url,
			digest_sha256 = excluded.digest_sha256, service_state = 'candidate',
			source_type = excluded.source_type, source_asset_key = excluded.source_asset_key,
			variant = excluded.variant, display_label = excluded.display_label,
			priority = excluded.priority, labels_json = excluded.labels_json,
			classification_reason = excluded.classification_reason`,
			assetID, releaseID, asset.NumericID, asset.FileName, classification.Architecture,
			classification.System, asset.SizeBytes, asset.DownloadURL, digest, now,
			asset.SourceType, asset.SourceAssetKey, classification.Variant,
			classification.DisplayLabel, classification.Priority, labelsJSON,
			classification.ClassificationReason)
		if err != nil {
			return accepted, rejected, err
		}
		acceptedIDs[asset.NumericID] = struct{}{}
		if logger != nil {
			logger.Debug(ctx, "资产已进入镜像候选",
				slog.String("project_id", project.ID),
				slog.String("release_id", releaseID),
				slog.String("asset_id", assetID),
				slog.String("asset_name", asset.FileName),
				slog.String("architecture", classification.Architecture),
				slog.String("system", classification.System),
				slog.String("variant", classification.Variant),
				slog.String("digest_sha256", digest))
		}
		accepted++
	}
	if err := markUnacceptedReleaseAssetsRemoved(ctx, tx, releaseID, acceptedIDs, now); err != nil {
		return accepted, rejected, err
	}
	return accepted, rejected, nil
}

func markInventoryStaleOnAssetChange(ctx context.Context, tx *sql.Tx, assetID, digest string, size int64, now string) error {
	var oldDigest string
	var oldSize int64
	err := tx.QueryRowContext(ctx, `SELECT digest_sha256, size_bytes FROM assets
		WHERE id = ?`, assetID).Scan(&oldDigest, &oldSize)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if oldDigest == digest && oldSize == size {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_inventory SET state = 'stale',
		verified_at = ? WHERE asset_id = ? AND state = 'verified'`, now, assetID)
	return err
}

func supersedeDuplicatePublicPaths(ctx context.Context, tx *sql.Tx, projectID string) error {
	return assetstate.ReconcilePublicPaths(ctx, tx, projectID)
}

func classificationLabelsJSON(labels []string) (string, error) {
	if len(labels) == 0 {
		return "", nil
	}
	data, err := json.Marshal(labels)
	return string(data), err
}

func selectReleases(releases []ResourceVersion, includePrerelease bool, keep int) []ResourceVersion {
	var selected []ResourceVersion
	for _, rel := range releases {
		if rel.Draft || (rel.Prerelease && !includePrerelease) {
			continue
		}
		selected = append(selected, rel)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].PublishedAt.Equal(selected[j].PublishedAt) {
			return selected[i].NumericID > selected[j].NumericID
		}
		return selected[i].PublishedAt.After(selected[j].PublishedAt)
	})
	if keep > 0 && len(selected) > keep {
		selected = selected[:keep]
	}
	return selected
}

func projectHash(project config.Project) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		project.ID, project.Repository,
		fmt.Sprint(project.Enabled), fmt.Sprint(project.RetainVersions),
		fmt.Sprint(project.IncludePrerelease), fmt.Sprint(project.DownloadMultiplier),
		assetRulesHash(project.AssetInclude),
		assetRulesHash(project.AssetExclude),
		project.ClassifyArchitectureRegex(),
		fmt.Sprint(project.ClassifyArchitectureEnabled()),
		fmt.Sprint(project.ClassifySystemEnabled()),
		project.ClassifySystemRegex(),
		assetPipelineHash(project.AssetPipeline),
	}, "|")))
	return hex.EncodeToString(sum[:])
}

func assetPipelineHash(pipeline config.AssetPipeline) string {
	data, err := json.Marshal(pipeline)
	if err != nil {
		return fmt.Sprintf("%+v", pipeline)
	}
	return string(data)
}

func compileArchitectureRegex(project config.Project) (*regexp.Regexp, error) {
	if !project.RegexClassificationEnabled() || !project.ClassifyArchitectureEnabled() {
		return nil, nil
	}
	return regexp.Compile(project.ClassifyArchitectureRegex())
}

func assetArchitecture(name string, archRE *regexp.Regexp) string {
	if archRE == nil {
		return ""
	}
	matches := archRE.FindStringSubmatch(name)
	if len(matches) == 0 {
		return "None"
	}
	return matches[len(matches)-1]
}

func compileSystemRegex(project config.Project) (*regexp.Regexp, error) {
	if !project.RegexClassificationEnabled() || !project.ClassifySystemEnabled() {
		return nil, nil
	}
	return regexp.Compile(project.ClassifySystemRegex())
}

func assetSystem(name string, systemRE *regexp.Regexp) string {
	if systemRE == nil {
		return ""
	}
	matches := systemRE.FindStringSubmatch(name)
	if len(matches) == 0 {
		return "None"
	}
	system, ok := normalizeSystem(matches[len(matches)-1])
	if !ok {
		return "None"
	}
	return system
}

func normalizeSystem(value string) (string, bool) {
	return config.NormalizeAssetSystem(value)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
