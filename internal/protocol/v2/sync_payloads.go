package v2

import "time"

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
	TaskID           string `json:"task_id"`
	AttemptID        string `json:"attempt_id"`
	Reason           string `json:"reason"`
	Code             string `json:"code,omitempty"`
	CapacityRevision uint64 `json:"capacity_revision,omitempty"`
}

const (
	SyncRejectedSlotsFull               = "slots_full"
	SyncRejectedPreviousAttemptStopping = "previous_attempt_stopping"
	SyncRejectedExecutorUnavailable     = "executor_unavailable"
)

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
