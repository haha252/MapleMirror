package public

import (
	"context"
	"database/sql"

	"mirror-server/internal/assetpath"
)

func (s Store) Projects(ctx context.Context) ([]ProjectSummary, error) {
	readCtx, cancel := stableDatabaseReadContext(ctx)
	defer cancel()
	rows, err := s.DB.QueryContext(readCtx, `SELECT p.id, p.repository, p.name,
		COALESCE(p.description, ''), COALESCE(p.homepage_url, ''),
		EXISTS(SELECT 1 FROM releases r JOIN assets a ON a.release_id = r.id`+routableAssetReplicaSQL+`
			WHERE r.project_id = p.id AND p.enabled = 1 AND r.selected = 1
			AND a.service_state = 'candidate') AS available,
		COALESCE(MAX(CASE WHEN r.selected = 1 THEN r.published_at ELSE '' END), '')
		FROM projects p LEFT JOIN releases r ON r.project_id = p.id
		WHERE p.enabled = 1 GROUP BY p.id, p.repository, p.name,
		p.description, p.homepage_url ORDER BY p.name, p.id`,
		s.routableAssetReplicaArgs()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectSummary
	for rows.Next() {
		var item ProjectSummary
		var available int
		if err := rows.Scan(&item.ProjectID, &item.Repository, &item.DisplayName,
			&item.Description, &item.HomepageURL, &available,
			&item.LatestPublishedAt); err != nil {
			return nil, err
		}
		item.Available = available == 1
		out = append(out, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Available {
			out[i].Available = s.projectHasRoutableAsset(readCtx, out[i].ProjectID)
		}
		if !out[i].Available {
			info := s.projectUnavailableInfo(readCtx, out[i].ProjectID)
			out[i].UnavailableReason = info.Summary
			out[i].UnavailableDetails = info.Detail
		}
	}
	return out, nil
}

func (s Store) Assets(ctx context.Context, projectID string) ([]AssetSummary, error) {
	readCtx, cancel := stableDatabaseReadContext(ctx)
	defer cancel()
	args := append(s.routableAssetReplicaArgs(), projectID)
	rows, err := s.DB.QueryContext(readCtx, `SELECT a.id, r.tag_name, r.prerelease,
		a.file_name, a.architecture, a.system, a.variant, a.display_label,
		a.priority, a.size_bytes, a.digest_sha256,
		EXISTS(SELECT 1 FROM node_inventory ni
			JOIN target_inventory ti ON ti.node_id = ni.node_id
				AND ti.asset_id = a.id
				AND ti.desired_state = 'required'
			JOIN nodes n ON n.id = ni.node_id
				AND n.state NOT IN ('disabled', 'offline')
				AND n.last_heartbeat_at IS NOT NULL
				AND n.last_heartbeat_at != ''
				AND n.public_download_base_url != ''
				AND (? <= 0 OR n.public_probe_network_failures < ?)
			WHERE ni.asset_id = a.id AND ni.state = 'verified'
			AND ni.local_digest_sha256 = a.digest_sha256
			AND ni.size_bytes = a.size_bytes) AS available,
		COALESCE(r.published_at, '')
		FROM assets a JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ? AND r.selected = 1 AND a.service_state = 'candidate'
		ORDER BY r.published_at DESC, a.file_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AssetSummary
	for rows.Next() {
		var item AssetSummary
		var prerelease, available int
		if err := rows.Scan(&item.AssetID, &item.Version, &prerelease, &item.FileName,
			&item.Architecture, &item.System, &item.Variant, &item.DisplayLabel,
			&item.Priority, &item.SizeBytes, &item.DigestSHA256, &available,
			&item.PublishedAt); err != nil {
			return nil, err
		}
		item.Prerelease = prerelease == 1
		item.Available = available == 1
		item.DownloadPath = assetpath.PublicPath(projectID, item.Version, item.FileName)
		out = append(out, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Available {
			out[i].Available = s.assetHasRoutableReplica(readCtx, out[i].AssetID)
		}
		if !out[i].Available {
			info := s.assetUnavailableInfo(readCtx, out[i].AssetID)
			out[i].UnavailableReason = info.Summary
			out[i].UnavailableDetails = info.Detail
		}
	}
	return out, nil
}

func (s Store) DownloadAsset(ctx context.Context, assetID string) (DownloadAssetSummary, error) {
	return s.downloadAsset(ctx, `a.id = ?`, assetID)
}

func (s Store) DownloadAssetByPath(ctx context.Context, value string) (DownloadAssetSummary, error) {
	parts, err := assetpath.ParsePublicPath(value)
	if err != nil {
		return DownloadAssetSummary{}, err
	}
	return s.downloadAsset(ctx, `p.id = ? AND r.tag_name = ? AND a.file_name = ?`,
		parts.ProjectID, parts.Version, parts.FileName)
}

func (s Store) downloadAsset(ctx context.Context, where string, args ...any) (DownloadAssetSummary, error) {
	var item DownloadAssetSummary
	var available int
	queryArgs := append(s.routableAssetReplicaArgs(), args...)
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.name, p.repository, a.id,
		r.tag_name, a.file_name, a.architecture, a.system, a.variant,
		a.display_label, a.priority, a.size_bytes,
		EXISTS(SELECT 1 FROM node_inventory ni
			JOIN target_inventory ti ON ti.node_id = ni.node_id
				AND ti.asset_id = a.id
				AND ti.desired_state = 'required'
			JOIN nodes n ON n.id = ni.node_id
				AND n.state NOT IN ('disabled', 'offline')
				AND n.last_heartbeat_at IS NOT NULL
				AND n.last_heartbeat_at != ''
				AND n.public_download_base_url != ''
				AND (? <= 0 OR n.public_probe_network_failures < ?)
			WHERE ni.asset_id = a.id AND ni.state = 'verified'
			AND ni.local_digest_sha256 = a.digest_sha256
			AND ni.size_bytes = a.size_bytes) AS available
		FROM assets a JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id
		WHERE `+where+` AND p.enabled = 1 AND r.selected = 1
		AND a.service_state = 'candidate'`, queryArgs...)
	if err != nil {
		return DownloadAssetSummary{}, err
	}
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			_ = rows.Close()
			return DownloadAssetSummary{}, sql.ErrNoRows
		}
		if err := rows.Scan(&item.ProjectID, &item.ProjectName, &item.Repository, &item.AssetID,
			&item.Version, &item.FileName, &item.Architecture, &item.System,
			&item.Variant, &item.DisplayLabel, &item.Priority,
			&item.SizeBytes, &available); err != nil {
			_ = rows.Close()
			return DownloadAssetSummary{}, err
		}
	}
	if err := rows.Close(); err != nil {
		return DownloadAssetSummary{}, err
	}
	if err := rows.Err(); err != nil {
		return DownloadAssetSummary{}, err
	}
	if count != 1 {
		return DownloadAssetSummary{}, sql.ErrNoRows
	}
	item.Available = available == 1
	if item.Available {
		item.Available = s.assetHasRoutableReplica(ctx, item.AssetID)
	}
	item.DownloadPath = assetpath.PublicPath(item.ProjectID, item.Version, item.FileName)
	if !item.Available {
		info := s.assetUnavailableInfo(ctx, item.AssetID)
		item.UnavailableReason = info.Summary
		item.UnavailableDetails = info.Detail
	}
	return item, nil
}
