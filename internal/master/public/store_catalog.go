package public

import "context"

func (s Store) Projects(ctx context.Context) ([]ProjectSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.repository, p.name,
		EXISTS(SELECT 1 FROM releases r JOIN assets a ON a.release_id = r.id
			JOIN node_inventory ni ON ni.asset_id = a.id AND ni.state = 'verified'
			JOIN nodes n ON n.id = ni.node_id AND n.routing_ready = 1 AND n.state != 'disabled'
			AND n.public_download_base_url != ''
			WHERE r.project_id = p.id AND p.enabled = 1 AND r.selected = 1
			AND a.service_state = 'candidate') AS available,
		COALESCE(MAX(CASE WHEN r.selected = 1 THEN r.published_at ELSE '' END), '')
		FROM projects p LEFT JOIN releases r ON r.project_id = p.id
		WHERE p.enabled = 1 GROUP BY p.id, p.repository, p.name ORDER BY p.name, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectSummary
	for rows.Next() {
		var item ProjectSummary
		var available int
		if err := rows.Scan(&item.ProjectID, &item.Repository, &item.DisplayName, &available, &item.LatestPublishedAt); err != nil {
			return nil, err
		}
		item.Available = available == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if !out[i].Available {
			info := s.projectUnavailableInfo(ctx, out[i].ProjectID)
			out[i].UnavailableReason = info.Summary
			out[i].UnavailableDetails = info.Detail
		}
	}
	return out, nil
}

func (s Store) Assets(ctx context.Context, projectID string) ([]AssetSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id, r.tag_name, r.prerelease,
		a.file_name, a.architecture, a.size_bytes, a.digest_sha256,
		EXISTS(SELECT 1 FROM node_inventory ni JOIN nodes n ON n.id = ni.node_id
			WHERE ni.asset_id = a.id AND ni.state = 'verified'
			AND n.routing_ready = 1 AND n.state != 'disabled'
			AND n.public_download_base_url != '') AS available,
		COALESCE(r.published_at, '')
		FROM assets a JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ? AND r.selected = 1 AND a.service_state = 'candidate'
		ORDER BY r.published_at DESC, a.file_name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AssetSummary
	for rows.Next() {
		var item AssetSummary
		var prerelease, available int
		if err := rows.Scan(&item.AssetID, &item.Version, &prerelease, &item.FileName,
			&item.Architecture, &item.SizeBytes, &item.DigestSHA256, &available, &item.PublishedAt); err != nil {
			return nil, err
		}
		item.Prerelease = prerelease == 1
		item.Available = available == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if !out[i].Available {
			info := s.assetUnavailableInfo(ctx, out[i].AssetID)
			out[i].UnavailableReason = info.Summary
			out[i].UnavailableDetails = info.Detail
		}
	}
	return out, nil
}
