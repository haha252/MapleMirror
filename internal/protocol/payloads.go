package protocol

import "time"

type EnrollRequest struct {
	PairingCode          string   `json:"pairing_code"`
	PublicName           string   `json:"public_name"`
	CSRPem               string   `json:"csr_pem"`
	PublicKeyFingerprint string   `json:"public_key_fingerprint"`
	Capabilities         []string `json:"capabilities"`
}

type EnrollPending struct {
	EnrollmentID      string    `json:"enrollment_id"`
	ExpiresAt         time.Time `json:"expires_at"`
	RetryAfterSeconds int       `json:"retry_after_seconds"`
	Message           string    `json:"message"`
}

type EnrollCertificateRequest struct {
	EnrollmentID string `json:"enrollment_id"`
}

type EnrollCertificate struct {
	EnrollmentID              string    `json:"enrollment_id"`
	NodeID                    string    `json:"node_id"`
	CertificatePEM            string    `json:"certificate_pem"`
	CAChainPEM                string    `json:"ca_chain_pem"`
	DownloadTokenPublicKeyPEM string    `json:"download_token_public_key_pem"`
	NotAfter                  time.Time `json:"not_after"`
}

type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Hello struct {
	LastAckSequence uint64   `json:"last_ack_sequence"`
	Capabilities    []string `json:"capabilities"`
	SoftwareVersion string   `json:"software_version"`
}

type Welcome struct {
	SessionID               string `json:"session_id"`
	AcceptedSequence        uint64 `json:"accepted_sequence"`
	HeartbeatIntervalSecond int    `json:"heartbeat_interval_seconds"`
	HeartbeatTimeoutSecond  int    `json:"heartbeat_timeout_seconds"`
	ManagedState            string `json:"managed_state"`
	RoutingReady            bool   `json:"routing_ready"`
}

type HeartbeatAckPayload struct {
	AcceptedSequence uint64                `json:"accepted_sequence"`
	ServerTime       time.Time             `json:"server_time"`
	ManagedState     string                `json:"managed_state"`
	RoutingReady     bool                  `json:"routing_ready"`
	PublicProbe      *PublicProbeChallenge `json:"public_probe,omitempty"`
}

type PublicProbeChallenge struct {
	ChallengeID string    `json:"challenge_id"`
	Nonce       string    `json:"nonce"`
	ExpiresAt   time.Time `json:"expires_at"`
	Algorithm   string    `json:"algorithm"`
}

type PublicProbeResponse struct {
	NodeID      string    `json:"node_id"`
	ChallengeID string    `json:"challenge_id"`
	Nonce       string    `json:"nonce"`
	ExpiresAt   time.Time `json:"expires_at"`
	Signature   string    `json:"signature"`
}

type PublicProbeReady struct {
	ChallengeID string `json:"challenge_id"`
}

type Heartbeat struct {
	Status                  string         `json:"status"`
	UptimeSeconds           uint64         `json:"uptime_seconds"`
	ActiveDownloads         int64          `json:"active_downloads"`
	FreeBytes               int64          `json:"free_bytes"`
	PublicDownloadBaseURL   string         `json:"public_download_base_url"`
	MaxMirrorProjects       int            `json:"max_mirror_projects"`
	SyncTaskSlotsAvailable  *int           `json:"sync_task_slots_available,omitempty"`
	Pressure                PressureSample `json:"pressure"`
	InventoryDigest         string         `json:"inventory_digest"`
	InventoryReportRevision uint64         `json:"inventory_report_revision"`
}

type PressureSample struct {
	TargetBandwidthBPS int64   `json:"target_bandwidth_bps"`
	ActualBandwidthBPS int64   `json:"actual_bandwidth_bps"`
	Ratio              float64 `json:"ratio"`
}

type InventoryReport struct {
	ReportID               string          `json:"report_id"`
	Revision               uint64          `json:"revision"`
	GeneratedAt            time.Time       `json:"generated_at"`
	Complete               bool            `json:"complete"`
	Items                  []InventoryItem `json:"items"`
	SyncTaskSlotsAvailable *int            `json:"sync_task_slots_available,omitempty"`
}

type InventoryItem struct {
	AssetID      string `json:"asset_id"`
	SizeBytes    int64  `json:"size_bytes"`
	DigestSHA256 string `json:"digest_sha256"`
	LocalState   string `json:"local_state"`
}

type PressureReport struct {
	ReportID               string    `json:"report_id"`
	SampledAt              time.Time `json:"sampled_at"`
	SampleWindowSeconds    int64     `json:"sample_window_seconds"`
	TargetBandwidthBPS     int64     `json:"target_bandwidth_bps"`
	ActualBandwidthBPS     int64     `json:"actual_bandwidth_bps"`
	PressureRatio          float64   `json:"pressure_ratio"`
	ActiveDownloads        int64     `json:"active_downloads"`
	FreeBytes              int64     `json:"free_bytes"`
	MaxMirrorProjects      int       `json:"max_mirror_projects"`
	SyncTaskSlotsAvailable *int      `json:"sync_task_slots_available,omitempty"`
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

type SyncFallbackSource struct {
	NodeID      string             `json:"node_id"`
	NodeName    string             `json:"node_name,omitempty"`
	DownloadURL string             `json:"download_url"`
	Token       string             `json:"token"`
	Parts       []SyncFallbackPart `json:"parts,omitempty"`
}

type SyncFallbackPart struct {
	RangeStart int64  `json:"range_start"`
	RangeEnd   int64  `json:"range_end"`
	Token      string `json:"token"`
}

type SyncTask struct {
	TaskID            string               `json:"task_id"`
	TaskType          string               `json:"task_type"`
	Asset             SyncAsset            `json:"asset"`
	FallbackSources   []SyncFallbackSource `json:"fallback_sources,omitempty"`
	RetryAfterSeconds int                  `json:"retry_after_seconds"`
}

type SyncTaskAck struct {
	TaskID                 string `json:"task_id"`
	State                  string `json:"state"`
	Message                string `json:"message,omitempty"`
	SyncTaskSlotsAvailable *int   `json:"sync_task_slots_available,omitempty"`
}

type SyncTaskResult struct {
	TaskID                 string `json:"task_id"`
	AssetID                string `json:"asset_id"`
	Result                 string `json:"result"`
	LocalDigestSHA256      string `json:"local_digest_sha256,omitempty"`
	SizeBytes              int64  `json:"size_bytes,omitempty"`
	Message                string `json:"message,omitempty"`
	PeerFallbackAttempted  bool   `json:"peer_fallback_attempted,omitempty"`
	SyncTaskSlotsAvailable *int   `json:"sync_task_slots_available,omitempty"`
}

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
	AcceptedSequence uint64 `json:"accepted_sequence"`
	Duplicate        bool   `json:"duplicate"`
	Message          string `json:"message"`
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
	AcceptedSequence uint64 `json:"accepted_sequence"`
	AuthorizationID  string `json:"authorization_id"`
	Message          string `json:"message"`
}

type AuthorizationStatusEvent struct {
	AuthorizationID string    `json:"authorization_id"`
	AssetID         string    `json:"asset_id"`
	Status          string    `json:"status"`
	Reason          string    `json:"reason,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
}

type AuthorizationStatusAck struct {
	AcceptedSequence uint64 `json:"accepted_sequence"`
	Message          string `json:"message"`
}
