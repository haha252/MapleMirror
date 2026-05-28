package public

import (
	"context"
	"database/sql"
)

type Store struct{ DB *sql.DB }

type ProjectSummary struct {
	ProjectID   string `json:"project_id"`
	Repository  string `json:"repository"`
	DisplayName string `json:"display_name"`
	Available   bool   `json:"available"`
}

type AssetSummary struct {
	AssetID           string `json:"asset_id"`
	Version           string `json:"version"`
	Prerelease        bool   `json:"prerelease"`
	FileName          string `json:"file_name"`
	Architecture      string `json:"architecture"`
	SizeBytes         int64  `json:"size_bytes"`
	DigestSHA256      string `json:"digest_sha256"`
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason"`
}

type NodeSummary struct {
	NodeID        string `json:"node_id"`
	PublicName    string `json:"public_name"`
	State         string `json:"state"`
	RoutingReady  bool   `json:"routing_ready"`
	LastHeartbeat string `json:"last_heartbeat_at,omitempty"`
	SLAEnabled    bool   `json:"sla_enabled"`
}

func (s Store) Projects(ctx context.Context) ([]ProjectSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.repository, p.name,
		EXISTS(SELECT 1 FROM releases r JOIN assets a ON a.release_id = r.id
			JOIN node_inventory ni ON ni.asset_id = a.id AND ni.state = 'verified'
			JOIN nodes n ON n.id = ni.node_id AND n.routing_ready = 1 AND n.state != 'disabled'
			WHERE r.project_id = p.id AND p.enabled = 1 AND r.selected = 1
			AND a.service_state = 'candidate') AS available
		FROM projects p WHERE p.enabled = 1 ORDER BY p.name, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectSummary
	for rows.Next() {
		var p ProjectSummary
		var available int
		if err := rows.Scan(&p.ProjectID, &p.Repository, &p.DisplayName, &available); err != nil {
			return nil, err
		}
		p.Available = available == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s Store) Assets(ctx context.Context, projectID string) ([]AssetSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id, r.tag_name, r.prerelease,
		a.file_name, a.architecture, a.size_bytes, a.digest_sha256,
		EXISTS(SELECT 1 FROM node_inventory ni JOIN nodes n ON n.id = ni.node_id
			WHERE ni.asset_id = a.id AND ni.state = 'verified'
			AND n.routing_ready = 1 AND n.state != 'disabled') AS available
		FROM assets a JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ? AND r.selected = 1 AND a.service_state = 'candidate'
		ORDER BY r.published_at DESC, a.file_name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AssetSummary
	for rows.Next() {
		var a AssetSummary
		var prerelease, available int
		err := rows.Scan(&a.AssetID, &a.Version, &prerelease, &a.FileName,
			&a.Architecture, &a.SizeBytes, &a.DigestSHA256, &available)
		if err != nil {
			return nil, err
		}
		a.Prerelease, a.Available = prerelease == 1, available == 1
		if !a.Available {
			a.UnavailableReason = "暂不可下载"
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s Store) Nodes(ctx context.Context) ([]NodeSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, public_name, state,
		routing_ready, COALESCE(last_heartbeat_at, '') FROM nodes ORDER BY public_name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeSummary
	for rows.Next() {
		var n NodeSummary
		var ready int
		if err := rows.Scan(&n.NodeID, &n.PublicName, &n.State,
			&ready, &n.LastHeartbeat); err != nil {
			return nil, err
		}
		n.RoutingReady = ready == 1
		out = append(out, n)
	}
	return out, rows.Err()
}
