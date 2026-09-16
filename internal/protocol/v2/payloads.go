package v2

import "time"

type Hello struct {
	SoftwareVersion string   `json:"software_version"`
	Capabilities    []string `json:"capabilities,omitempty"`
}

type Welcome struct {
	SessionID               string    `json:"session_id"`
	ServerTime              time.Time `json:"server_time"`
	StatusIntervalSeconds   int       `json:"status_interval_seconds"`
	HeartbeatTimeoutSeconds int       `json:"heartbeat_timeout_seconds"`
	ManagedState            string    `json:"managed_state"`
	RoutingReady            bool      `json:"routing_ready"`
}

type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type FilesystemCapacity struct {
	AvailableBytes int64 `json:"available_bytes"`
	TotalBytes     int64 `json:"total_bytes"`
	ReservedBytes  int64 `json:"reserved_bytes,omitempty"`
	Valid          bool  `json:"valid"`
}

type ActiveTask struct {
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id"`
}

type NodeStatus struct {
	Status                 string             `json:"status"`
	UptimeSeconds          uint64             `json:"uptime_seconds"`
	PublicDownloadBaseURL  string             `json:"public_download_base_url"`
	MaxMirrorProjects      int                `json:"max_mirror_projects"`
	SyncTaskSlotsAvailable int                `json:"sync_task_slots_available"`
	TargetBandwidthBPS     int64              `json:"target_bandwidth_bps"`
	ActualBandwidthBPS     int64              `json:"actual_bandwidth_bps"`
	PublicActiveDownloads  int64              `json:"public_active_downloads"`
	SwarmActiveUploads     int64              `json:"swarm_active_uploads"`
	AssetFS                FilesystemCapacity `json:"asset_fs"`
	PartialFS              FilesystemCapacity `json:"partial_fs"`
	ActiveTasks            []ActiveTask       `json:"active_tasks,omitempty"`
}

type SyncAsset struct {
	AssetID      string `json:"asset_id"`
	ProjectID    string `json:"project_id,omitempty"`
	Version      string `json:"version,omitempty"`
	FileName     string `json:"file_name"`
	SizeBytes    int64  `json:"size_bytes"`
	DownloadURL  string `json:"download_url"`
	DigestSHA256 string `json:"digest_sha256"`
}

type SyncTask struct {
	TaskID         string         `json:"task_id"`
	AttemptID      string         `json:"attempt_id"`
	TaskType       string         `json:"task_type"`
	Asset          SyncAsset      `json:"asset"`
	LeaseExpiresAt time.Time      `json:"lease_expires_at"`
	Bootstrap      bool           `json:"bootstrap,omitempty"`
	SwarmDisabled  bool           `json:"swarm_disabled,omitempty"`
	Manifest       *SwarmManifest `json:"manifest,omitempty"`
	Sources        []SwarmSource  `json:"sources,omitempty"`
	WholeSources   []WholeSource  `json:"whole_sources,omitempty"`
}

type SyncAccepted struct {
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id"`
}

type SyncRejected struct {
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id"`
	Reason    string `json:"reason"`
}

type SyncCancel struct {
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type SyncResult struct {
	TaskID            string `json:"task_id"`
	AttemptID         string `json:"attempt_id"`
	AssetID           string `json:"asset_id,omitempty"`
	Result            string `json:"result"`
	LocalDigestSHA256 string `json:"local_digest_sha256,omitempty"`
	SizeBytes         int64  `json:"size_bytes,omitempty"`
	Message           string `json:"message,omitempty"`
}

type SyncResultAck struct {
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id"`
	Accepted  bool   `json:"accepted"`
}

type InventoryItem struct {
	AssetID      string `json:"asset_id"`
	SizeBytes    int64  `json:"size_bytes"`
	DigestSHA256 string `json:"digest_sha256"`
	LocalState   string `json:"local_state"`
}

type InventorySnapshotSegment struct {
	Revision    uint64          `json:"revision"`
	Segment     int             `json:"segment"`
	Complete    bool            `json:"complete"`
	GeneratedAt time.Time       `json:"generated_at"`
	Items       []InventoryItem `json:"items"`
}

type InventorySnapshotAck struct {
	Revision uint64 `json:"revision"`
}

type SwarmAvailability struct {
	AssetID    string `json:"asset_id"`
	ManifestID string `json:"manifest_id"`
	Revision   uint64 `json:"revision"`
	Bitset     []byte `json:"bitset"`
}

// Accounting/authorization payloads deliberately keep their domain fields from v1;
// transport sequencing is removed in control.v2.
type TrafficEvent struct {
	EventSequence   uint64    `json:"event_sequence"`
	AuthorizationID string    `json:"authorization_id"`
	AssetID         string    `json:"asset_id"`
	NodeRequestID   string    `json:"node_request_id"`
	MasterRequestID string    `json:"master_request_id"`
	SentBytes       int64     `json:"sent_bytes"`
	Status          string    `json:"status"`
	ReportedAt      time.Time `json:"reported_at"`
}

type TrafficEventAck struct {
	EventSequence uint64 `json:"event_sequence"`
	Duplicate     bool   `json:"duplicate"`
}

type DownloadAuthorization struct {
	AuthorizationID        string    `json:"authorization_id"`
	TokenHash              string    `json:"token_hash"`
	AssetID                string    `json:"asset_id"`
	NodeID                 string    `json:"node_id"`
	ClientPrefix           string    `json:"client_prefix"`
	IssuedAt               time.Time `json:"issued_at"`
	ExpiresAt              time.Time `json:"expires_at"`
	FirstConnectionSeconds int       `json:"first_connection_timeout_seconds"`
	IdleTimeoutSeconds     int       `json:"idle_timeout_seconds"`
	MaxDurationSeconds     int       `json:"max_duration_seconds"`
	MaxBytes               int64     `json:"max_bytes"`
	TrafficLimitBytes      int64     `json:"traffic_limit_bytes"`
	RangeConcurrencyLimit  int       `json:"range_concurrency_limit"`
	RequestID              string    `json:"request_id"`
}

type DownloadAuthorizationAck struct {
	AuthorizationID string `json:"authorization_id"`
}

type AuthorizationStatus struct {
	AuthorizationID string    `json:"authorization_id"`
	AssetID         string    `json:"asset_id"`
	Status          string    `json:"status"`
	Reason          string    `json:"reason,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
}

type AuthorizationStatusAck struct {
	AuthorizationID string `json:"authorization_id"`
	Status          string `json:"status"`
}

type PublicProbeChallenge struct {
	ChallengeID string    `json:"challenge_id"`
	Nonce       string    `json:"nonce"`
	ExpiresAt   time.Time `json:"expires_at"`
	Algorithm   string    `json:"algorithm"`
}

type PublicProbeReady struct {
	ChallengeID string `json:"challenge_id"`
}

type SwarmManifest struct {
	ManifestID         string `json:"manifest_id"`
	AssetID            string `json:"asset_id"`
	AssetSize          int64  `json:"asset_size"`
	AssetSHA256        string `json:"asset_sha256"`
	PieceLayoutVersion int    `json:"piece_layout_version"`
	PieceSize          int64  `json:"piece_size"`
	PieceCount         int    `json:"piece_count"`
	PieceHashAlgorithm string `json:"piece_hash_algorithm"`
	PieceHashes        []byte `json:"piece_hash_blob"`
}

type SwarmManifestRequest struct {
	AssetID string `json:"asset_id"`
}
type SwarmManifestAck struct {
	ManifestID string `json:"manifest_id"`
	AssetID    string `json:"asset_id"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
}

type SwarmSource struct {
	NodeID       string `json:"node_id"`
	BaseURL      string `json:"base_url"`
	Capability   string `json:"capability"`
	Availability []byte `json:"availability"`
	Complete     bool   `json:"complete"`
}

type SwarmSources struct {
	TaskID     string        `json:"task_id"`
	AttemptID  string        `json:"attempt_id"`
	AssetID    string        `json:"asset_id"`
	ManifestID string        `json:"manifest_id"`
	Sources    []SwarmSource `json:"sources"`
}

type SwarmSourcesRequest struct {
	TaskID     string `json:"task_id"`
	AttemptID  string `json:"attempt_id"`
	AssetID    string `json:"asset_id"`
	ManifestID string `json:"manifest_id"`
}
