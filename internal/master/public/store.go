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
var errChallengeOutstanding = errors.New("当前来源待完成挑战过多")
var errChallengeCapacity = errors.New("全局待完成挑战已满")
var errVDFBusy = errors.New("VDF 挑战创建繁忙")

type Store struct {
	DB                         *sql.DB
	Quota                      quotaPolicy
	Location                   *time.Location
	Challenges                 *challengeMemory
	PoWDifficulty              powSizePolicy
	VDF                        *vdfService
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
	source, algorithm := "web", "altcha-sha256-v1"
	if kind == "api_pow" {
		source, algorithm = "api", "sha256"
	}
	return s.createChallenge(ctx, source, "v1", algorithm, assetID, prefix,
		func(int64) int { return difficulty }, ttl)
}

func (s *Store) CreatePoWChallenge(ctx context.Context, kind, assetID, prefix string,
	additionalBits int, ttl time.Duration) (Challenge, error) {
	if len(s.PoWDifficulty.tiers) == 0 {
		return Challenge{}, errors.New("普通 PoW 大小分档未初始化")
	}
	source, algorithm := "web", "altcha-sha256-v1"
	if kind == "api_pow" {
		source, algorithm = "api", "sha256"
	}
	return s.createChallenge(ctx, source, "v1", algorithm, assetID, prefix,
		func(sizeBytes int64) int {
			return s.PoWDifficulty.difficulty(sizeBytes, additionalBits)
		}, ttl)
}

func (s *Store) createChallenge(ctx context.Context, source, version, algorithm, assetID, prefix string,
	difficultyForSize func(int64) int, ttl time.Duration) (Challenge, error) {
	now := time.Now().UTC()
	challenges := s.challengeMemory()
	if err := challenges.reserve(prefix, now); err != nil {
		return Challenge{}, err
	}
	committed := false
	defer func() {
		if !committed {
			challenges.releaseReservation(prefix)
		}
	}()
	id, err := requestid.New()
	if err != nil {
		return Challenge{}, err
	}
	sizeBytes, err := s.routableAsset(ctx, assetID)
	if err != nil {
		return Challenge{}, err
	}
	challenge := Challenge{
		ID:              id,
		SourceKind:      source,
		ProtocolVersion: version,
		Algorithm:       algorithm,
		AssetID:         assetID,
		AssetSizeBytes:  sizeBytes,
		ClientPrefixKey: prefix,
		CreatedAt:       now.Format(time.RFC3339Nano),
		Nonce:           randomText(16),
		Difficulty:      difficultyForSize(sizeBytes),
		ExpiresAt:       now.Add(ttl).Format(time.RFC3339Nano),
	}
	challenges.put(challenge, now)
	committed = true
	return challenge, nil
}

func (s *Store) LoadChallenge(_ context.Context, id string) (Challenge, error) {
	challenge, ok := s.challengeMemory().get(id, time.Now().UTC())
	if !ok {
		return Challenge{}, sql.ErrNoRows
	}
	return challenge, nil
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
