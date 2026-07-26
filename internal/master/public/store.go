package public

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/downloadurl"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/accountingarchive"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/statbuffer"
	"mirror-server/internal/requestid"
)

var errChallengeQuota = errors.New("挑战创建过于频繁")

type Store struct {
	DB                         *sql.DB
	Quota                      quotaPolicy
	Location                   *time.Location
	Challenges                 *challengeMemory
	MaxBytes                   maxBytesPolicy
	RangeLimit                 int
	Runtime                    *mastercontrol.RuntimeStore
	PublicProbeNetworkFailures int
	Archive                    *accountingarchive.Writer
	Logger                     *logging.Logger
	StatsBuffer                *statbuffer.Buffer
}

type ProjectSummary struct {
	ProjectID          string `json:"project_id"`
	Repository         string `json:"repository"`
	DisplayName        string `json:"display_name"`
	Description        string `json:"description"`
	HomepageURL        string `json:"homepage_url"`
	Available          bool   `json:"available"`
	LatestPublishedAt  string `json:"-"`
	UnavailableReason  string `json:"-"`
	UnavailableDetails string `json:"-"`
}

type AssetSummary struct {
	AssetID            string `json:"asset_id"`
	Version            string `json:"version"`
	DownloadPath       string `json:"download_path"`
	Prerelease         bool   `json:"prerelease"`
	FileName           string `json:"file_name"`
	Architecture       string `json:"architecture"`
	System             string `json:"system"`
	Variant            string `json:"variant,omitempty"`
	DisplayLabel       string `json:"display_label,omitempty"`
	Priority           int    `json:"priority,omitempty"`
	SizeBytes          int64  `json:"size_bytes"`
	DigestSHA256       string `json:"digest_sha256"`
	Available          bool   `json:"available"`
	PublishedAt        string `json:"-"`
	UnavailableReason  string `json:"unavailable_reason"`
	UnavailableDetails string `json:"-"`
}

type DownloadAssetSummary struct {
	ProjectID          string `json:"project_id"`
	ProjectName        string `json:"project_name"`
	Repository         string `json:"repository"`
	AssetID            string `json:"asset_id"`
	Version            string `json:"version"`
	DownloadPath       string `json:"download_path"`
	FileName           string `json:"file_name"`
	Architecture       string `json:"architecture"`
	System             string `json:"system"`
	Variant            string `json:"variant,omitempty"`
	DisplayLabel       string `json:"display_label,omitempty"`
	Priority           int    `json:"priority,omitempty"`
	SizeBytes          int64  `json:"size_bytes"`
	Available          bool   `json:"available"`
	UnavailableReason  string `json:"unavailable_reason"`
	UnavailableDetails string `json:"-"`
}

type NodeSummary struct {
	NodeID                string `json:"node_id"`
	PublicName            string `json:"public_name"`
	State                 string `json:"state"`
	DownloadReady         bool   `json:"download_ready"`
	DownloadReadyReason   string `json:"-"`
	DownloadReadyDetails  string `json:"-"`
	RoutingReady          bool   `json:"routing_ready"`
	RoutingReadyReason    string `json:"-"`
	RoutingReadyDetails   string `json:"-"`
	LastHeartbeat         string `json:"last_heartbeat_at,omitempty"`
	SLA24H                string `json:"sla_24h"`
	SLA7D                 string `json:"sla_7d"`
	SLA30D                string `json:"sla_30d"`
	PressureRatio         string `json:"pressure_ratio"`
	TotalSentBytes        int64  `json:"total_sent_bytes"`
	PublicDownloadBaseURL string `json:"-"`
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
	NodeName        string
	ClientPrefixKey string
	State           string
	ExpiresAt       string
	TokenHash       string
}

func (s *Store) CreateChallenge(ctx context.Context, kind, assetID, prefix string, difficulty int, ttl time.Duration, _ string) (Challenge, error) {
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
	if _, err := s.routableAsset(ctx, assetID); err != nil {
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

func (s *Store) beginChallenge(id string) (Challenge, bool, bool) {
	return s.challengeMemory().begin(id, time.Now().UTC())
}

func (s *Store) finishChallenge(id string) {
	s.challengeMemory().finish(id)
}

func (s *Store) releaseChallenge(id string) {
	s.challengeMemory().release(id)
}

func (s Store) notifyAuthorizationDelivery(nodeID string) {
	if s.Runtime != nil {
		s.Runtime.NotifySyncTasks(nodeID)
	}
}

func (s *Store) challengeMemory() *challengeMemory {
	if s.Challenges == nil {
		s.Challenges = newChallengeMemory()
	}
	return s.Challenges
}

func (s Store) routableAsset(ctx context.Context, assetID string) (int64, error) {
	readCtx, cancel := stableDatabaseReadContext(ctx)
	defer cancel()
	args := append(s.routableAssetReplicaArgs(), assetID)
	rows, err := s.DB.QueryContext(readCtx, `SELECT a.size_bytes, n.public_download_base_url
		FROM assets a
		JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id`+routableAssetReplicaSQL+`
		WHERE a.id = ? AND a.service_state = 'candidate'
		AND r.selected = 1 AND p.enabled = 1`, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var size int64
		var baseURL string
		if err := rows.Scan(&size, &baseURL); err != nil {
			return 0, err
		}
		if _, ok := downloadurl.NormalizeBase(baseURL); ok {
			return size, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return 0, sql.ErrNoRows
}
