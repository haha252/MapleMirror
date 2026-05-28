package public

import (
	"context"
	"database/sql"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/requestid"
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

type Challenge struct {
	ID              string
	Kind            string
	AssetID         string
	ClientPrefixKey string
	Nonce           string
	Difficulty      int
	ExpiresAt       string
}

type IssuedAuthorization struct {
	Claims downloadtoken.Claims
}

type AuthorizationStatus struct {
	AuthorizationID string
	AssetID         string
	NodeID          string
	State           string
	ExpiresAt       string
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

func (s Store) CreateChallenge(ctx context.Context, kind, assetID, prefix string, difficulty int, ttl time.Duration, requestID string) (Challenge, error) {
	if _, err := s.routableAsset(ctx, assetID); err != nil {
		return Challenge{}, err
	}
	id, err := requestid.New()
	if err != nil {
		return Challenge{}, err
	}
	c := Challenge{ID: id, Kind: kind, AssetID: assetID, ClientPrefixKey: prefix,
		Nonce: randomText(16), Difficulty: difficulty, ExpiresAt: expiresAfter(ttl)}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO challenges
		(id, kind, asset_id, client_prefix_key, nonce_hash, difficulty,
		expires_at, request_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Kind, c.AssetID, c.ClientPrefixKey, c.Nonce,
		c.Difficulty, c.ExpiresAt, requestID)
	return c, err
}

func (s Store) LoadChallenge(ctx context.Context, id string) (Challenge, error) {
	var c Challenge
	err := s.DB.QueryRowContext(ctx, `SELECT id, kind, asset_id,
		client_prefix_key, nonce_hash, COALESCE(difficulty, 0), expires_at
		FROM challenges WHERE id = ? AND consumed_at IS NULL AND expires_at > ?`,
		id, nowText()).Scan(&c.ID, &c.Kind, &c.AssetID, &c.ClientPrefixKey,
		&c.Nonce, &c.Difficulty, &c.ExpiresAt)
	return c, err
}

func (s Store) IssueAuthorization(ctx context.Context, c Challenge, ttl time.Duration, reqID string) (IssuedAuthorization, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return IssuedAuthorization{}, err
	}
	defer tx.Rollback()
	nodeID, size, err := s.routableAssetTx(ctx, tx, c.AssetID)
	if err != nil {
		return IssuedAuthorization{}, err
	}
	authID, err := requestid.New()
	if err != nil {
		return IssuedAuthorization{}, err
	}
	expires := expiresAfter(ttl)
	_, err = tx.ExecContext(ctx, `INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
		max_bytes, range_limit, status, request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'issued', ?)`,
		authID, c.AssetID, nodeID, c.ClientPrefixKey, nowText(), expires, size, 4, reqID)
	if err != nil {
		return IssuedAuthorization{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE challenges SET consumed_at = ?
		WHERE id = ? AND consumed_at IS NULL`, nowText(), c.ID)
	if err != nil {
		return IssuedAuthorization{}, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return IssuedAuthorization{}, sql.ErrNoRows
	}
	claims := downloadtoken.Claims{TokenVersion: "download.v1", AuthorizationID: authID,
		AssetID: c.AssetID, NodeID: nodeID, ClientPrefix: c.ClientPrefixKey,
		ExpiresAt: expires, MaxBytes: size, RangeConcurrencyLimit: 4, RequestID: reqID}
	return IssuedAuthorization{Claims: claims}, tx.Commit()
}

func (s Store) Authorization(ctx context.Context, id string) (AuthorizationStatus, error) {
	var out AuthorizationStatus
	err := s.DB.QueryRowContext(ctx, `SELECT id, asset_id, node_id, status,
		expires_at FROM download_authorizations WHERE id = ?`, id).
		Scan(&out.AuthorizationID, &out.AssetID, &out.NodeID, &out.State, &out.ExpiresAt)
	return out, err
}

func (s Store) routableAsset(ctx context.Context, assetID string) (int64, error) {
	var size int64
	err := s.DB.QueryRowContext(ctx, `SELECT a.size_bytes FROM assets a
		JOIN node_inventory ni ON ni.asset_id = a.id AND ni.state = 'verified'
		JOIN nodes n ON n.id = ni.node_id AND n.routing_ready = 1 AND n.state != 'disabled'
		WHERE a.id = ? AND a.service_state = 'candidate' LIMIT 1`, assetID).Scan(&size)
	return size, err
}

func (s Store) routableAssetTx(ctx context.Context, tx *sql.Tx, assetID string) (string, int64, error) {
	var nodeID string
	var size int64
	err := tx.QueryRowContext(ctx, `SELECT n.id, a.size_bytes FROM assets a
		JOIN node_inventory ni ON ni.asset_id = a.id AND ni.state = 'verified'
		JOIN nodes n ON n.id = ni.node_id AND n.routing_ready = 1 AND n.state != 'disabled'
		WHERE a.id = ? AND a.service_state = 'candidate'
		ORDER BY COALESCE(n.last_heartbeat_at, '') DESC, n.id LIMIT 1`, assetID).
		Scan(&nodeID, &size)
	return nodeID, size, err
}
