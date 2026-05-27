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
	EnrollmentID   string    `json:"enrollment_id"`
	NodeID         string    `json:"node_id"`
	CertificatePEM string    `json:"certificate_pem"`
	CAChainPEM     string    `json:"ca_chain_pem"`
	NotAfter       time.Time `json:"not_after"`
}

type Welcome struct {
	SessionID               string `json:"session_id"`
	AcceptedSequence        uint64 `json:"accepted_sequence"`
	HeartbeatIntervalSecond int    `json:"heartbeat_interval_seconds"`
	HeartbeatTimeoutSecond  int    `json:"heartbeat_timeout_seconds"`
	ManagedState            string `json:"managed_state"`
	RoutingReady            bool   `json:"routing_ready"`
}

type Heartbeat struct {
	Status                  string         `json:"status"`
	UptimeSeconds           uint64         `json:"uptime_seconds"`
	ActiveDownloads         int64          `json:"active_downloads"`
	FreeBytes               int64          `json:"free_bytes"`
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
	ReportID    string          `json:"report_id"`
	Revision    uint64          `json:"revision"`
	GeneratedAt time.Time       `json:"generated_at"`
	Complete    bool            `json:"complete"`
	Items       []InventoryItem `json:"items"`
}

type InventoryItem struct {
	AssetID      string `json:"asset_id"`
	SizeBytes    int64  `json:"size_bytes"`
	DigestSHA256 string `json:"digest_sha256"`
	LocalState   string `json:"local_state"`
}

type PressureReport struct {
	ReportID            string    `json:"report_id"`
	SampledAt           time.Time `json:"sampled_at"`
	SampleWindowSeconds int64     `json:"sample_window_seconds"`
	TargetBandwidthBPS  int64     `json:"target_bandwidth_bps"`
	ActualBandwidthBPS  int64     `json:"actual_bandwidth_bps"`
	PressureRatio       float64   `json:"pressure_ratio"`
	ActiveDownloads     int64     `json:"active_downloads"`
	FreeBytes           int64     `json:"free_bytes"`
}
