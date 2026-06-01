package public

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/requestid"
)

var errChallengeQuota = errors.New("挑战创建过于频繁")

type Store struct {
	DB         *sql.DB
	Quota      quotaPolicy
	Location   *time.Location
	Challenges *challengeMemory
	MaxBytes   maxBytesPolicy
}

type ProjectSummary struct {
	ProjectID          string `json:"project_id"`
	Repository         string `json:"repository"`
	DisplayName        string `json:"display_name"`
	Available          bool   `json:"available"`
	LatestPublishedAt  string `json:"-"`
	UnavailableReason  string `json:"-"`
	UnavailableDetails string `json:"-"`
}

type AssetSummary struct {
	AssetID            string `json:"asset_id"`
	Version            string `json:"version"`
	Prerelease         bool   `json:"prerelease"`
	FileName           string `json:"file_name"`
	Architecture       string `json:"architecture"`
	System             string `json:"system"`
	SizeBytes          int64  `json:"size_bytes"`
	DigestSHA256       string `json:"digest_sha256"`
	Available          bool   `json:"available"`
	PublishedAt        string `json:"-"`
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
	TotalSentBytes      int64  `json:"total_sent_bytes"`
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

func (s *Store) CreateChallenge(ctx context.Context, kind, assetID, prefix string, difficulty int, ttl time.Duration, _ string) (Challenge, error) {
	if _, err := s.routableAsset(ctx, assetID); err != nil {
		return Challenge{}, err
	}
	now := time.Now().UTC()
	challenges := s.challengeMemory()
	challenges.cleanup(now)
	if !challenges.allow(kind, prefix, now) {
		return Challenge{}, errChallengeQuota
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
		ExpiresAt:       now.Add(ttl).Format(time.RFC3339Nano),
	}
	challenges.put(challenge, now)
	return challenge, nil
}

func (s *Store) LoadChallenge(_ context.Context, id string) (Challenge, error) {
	challenge, ok := s.challengeMemory().get(id, time.Now().UTC())
	if !ok {
		return Challenge{}, sql.ErrNoRows
	}
	return challenge, nil
}

func (s *Store) consumeChallenge(id string) bool {
	return s.challengeMemory().consume(id, time.Now().UTC())
}

func (s *Store) challengeMemory() *challengeMemory {
	if s.Challenges == nil {
		s.Challenges = newChallengeMemory()
	}
	return s.Challenges
}

func (s Store) routableAsset(ctx context.Context, assetID string) (int64, error) {
	var size int64
	err := s.DB.QueryRowContext(ctx, `SELECT a.size_bytes FROM assets a
		JOIN node_inventory ni ON ni.asset_id = a.id AND ni.state = 'verified'
		JOIN nodes n ON n.id = ni.node_id AND n.routing_ready = 1 AND n.state != 'disabled'
		AND n.public_download_base_url != ''
		WHERE a.id = ? AND a.service_state = 'candidate' LIMIT 1`, assetID).Scan(&size)
	return size, err
}
