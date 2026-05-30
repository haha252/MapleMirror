package public

import (
	"context"
	"database/sql"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/requestid"
)

type Store struct {
	DB       *sql.DB
	Quota    quotaPolicy
	Location *time.Location
}

type ProjectSummary struct {
	ProjectID          string `json:"project_id"`
	Repository         string `json:"repository"`
	DisplayName        string `json:"display_name"`
	Available          bool   `json:"available"`
	UnavailableReason  string `json:"-"`
	UnavailableDetails string `json:"-"`
}

type AssetSummary struct {
	AssetID            string `json:"asset_id"`
	Version            string `json:"version"`
	Prerelease         bool   `json:"prerelease"`
	FileName           string `json:"file_name"`
	Architecture       string `json:"architecture"`
	SizeBytes          int64  `json:"size_bytes"`
	DigestSHA256       string `json:"digest_sha256"`
	Available          bool   `json:"available"`
	UnavailableReason  string `json:"unavailable_reason"`
	UnavailableDetails string `json:"-"`
}

type NodeSummary struct {
	NodeID              string `json:"node_id"`
	PublicName          string `json:"public_name"`
	State               string `json:"state"`
	RoutingReady        bool   `json:"routing_ready"`
	RoutingReadyReason  string `json:"-"`
	RoutingReadyDetails string `json:"-"`
	LastHeartbeat       string `json:"last_heartbeat_at,omitempty"`
	SLA24H              string `json:"sla_24h"`
	SLA7D               string `json:"sla_7d"`
	SLA30D              string `json:"sla_30d"`
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
		var item ProjectSummary
		var available int
		if err := rows.Scan(&item.ProjectID, &item.Repository, &item.DisplayName, &available); err != nil {
			return nil, err
		}
		item.Available = available == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
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
		var item AssetSummary
		var prerelease, available int
		if err := rows.Scan(&item.AssetID, &item.Version, &prerelease, &item.FileName,
			&item.Architecture, &item.SizeBytes, &item.DigestSHA256, &available); err != nil {
			return nil, err
		}
		item.Prerelease = prerelease == 1
		item.Available = available == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
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

func (s Store) Nodes(ctx context.Context) ([]NodeSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, public_name, state,
		routing_ready, COALESCE(last_heartbeat_at, '') FROM nodes ORDER BY public_name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NodeSummary
	for rows.Next() {
		var item NodeSummary
		var ready int
		if err := rows.Scan(&item.NodeID, &item.PublicName, &item.State, &ready, &item.LastHeartbeat); err != nil {
			return nil, err
		}
		item.RoutingReady = ready == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if !out[i].RoutingReady {
			info := s.nodeRoutingReadyInfo(ctx, out[i].NodeID, out[i].State, out[i].LastHeartbeat)
			out[i].RoutingReadyReason = info.Summary
			out[i].RoutingReadyDetails = info.Detail
		}
		out[i].SLA24H = s.slaText(ctx, out[i].NodeID, 24)
		out[i].SLA7D = s.slaText(ctx, out[i].NodeID, 24*7)
		out[i].SLA30D = s.slaText(ctx, out[i].NodeID, 24*30)
	}
	return out, nil
}

func (s Store) CreateChallenge(ctx context.Context, kind, assetID, prefix string, difficulty int, ttl time.Duration, requestID string) (Challenge, error) {
	if _, err := s.routableAsset(ctx, assetID); err != nil {
		return Challenge{}, err
	}
	id, err := requestid.New()
	if err != nil {
		return Challenge{}, err
	}
	challenge := Challenge{
		ID:              id,
		Kind:            kind,
		AssetID:         assetID,
		ClientPrefixKey: prefix,
		Nonce:           randomText(16),
		Difficulty:      difficulty,
		ExpiresAt:       expiresAfter(ttl),
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO challenges
		(id, kind, asset_id, client_prefix_key, nonce_hash, difficulty,
		expires_at, request_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		challenge.ID, challenge.Kind, challenge.AssetID, challenge.ClientPrefixKey,
		challenge.Nonce, challenge.Difficulty, challenge.ExpiresAt, requestID)
	return challenge, err
}

func (s Store) LoadChallenge(ctx context.Context, id string) (Challenge, error) {
	var challenge Challenge
	err := s.DB.QueryRowContext(ctx, `SELECT id, kind, asset_id,
		client_prefix_key, nonce_hash, COALESCE(difficulty, 0), expires_at
		FROM challenges WHERE id = ? AND consumed_at IS NULL AND expires_at > ?`,
		id, nowText()).Scan(&challenge.ID, &challenge.Kind, &challenge.AssetID,
		&challenge.ClientPrefixKey, &challenge.Nonce, &challenge.Difficulty, &challenge.ExpiresAt)
	return challenge, err
}

func (s Store) routableAsset(ctx context.Context, assetID string) (int64, error) {
	var size int64
	err := s.DB.QueryRowContext(ctx, `SELECT a.size_bytes FROM assets a
		JOIN node_inventory ni ON ni.asset_id = a.id AND ni.state = 'verified'
		JOIN nodes n ON n.id = ni.node_id AND n.routing_ready = 1 AND n.state != 'disabled'
		WHERE a.id = ? AND a.service_state = 'candidate' LIMIT 1`, assetID).Scan(&size)
	return size, err
}
