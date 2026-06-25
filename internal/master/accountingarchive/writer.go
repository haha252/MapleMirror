package accountingarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const schemaVersion = 1

type Writer struct {
	root string
}

func New(root string) *Writer {
	if root == "" {
		return nil
	}
	return &Writer{root: root}
}

type AuthorizationRecord struct {
	AuthorizationID               string `json:"authorization_id"`
	AssetID                       string `json:"asset_id"`
	NodeID                        string `json:"node_id"`
	ClientPrefixKey               string `json:"client_prefix_key"`
	ProjectID                     string `json:"project_id"`
	System                        string `json:"system"`
	Architecture                  string `json:"architecture"`
	IssuedAt                      string `json:"issued_at"`
	ExpiresAt                     string `json:"expires_at"`
	MaxBytes                      int64  `json:"max_bytes"`
	TrafficLimitBytes             int64  `json:"traffic_limit_bytes"`
	RangeLimit                    int    `json:"range_limit"`
	RequestID                     string `json:"request_id"`
	TokenHash                     string `json:"token_hash"`
	FirstConnectionTimeoutSeconds int    `json:"first_connection_timeout_seconds"`
	IdleTimeoutSeconds            int    `json:"idle_timeout_seconds"`
	MaxDurationSeconds            int    `json:"max_duration_seconds"`
}

type TrafficRecord struct {
	NodeID          string    `json:"node_id"`
	EventSequence   uint64    `json:"event_sequence"`
	AuthorizationID string    `json:"authorization_id"`
	AssetID         string    `json:"asset_id"`
	NodeRequestID   string    `json:"node_request_id"`
	MasterRequestID string    `json:"master_request_id"`
	SentBytes       int64     `json:"sent_bytes"`
	Status          string    `json:"status"`
	ReportedAt      time.Time `json:"reported_at"`
	AccountedAt     string    `json:"accounted_at"`
}

type AuthorizationStatusRecord struct {
	NodeID          string    `json:"node_id"`
	AuthorizationID string    `json:"authorization_id"`
	AssetID         string    `json:"asset_id"`
	Status          string    `json:"status"`
	Reason          string    `json:"reason,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
}

func (w *Writer) WriteAuthorization(ctx context.Context, record AuthorizationRecord, committed time.Time) error {
	return w.write(ctx, "authorization", record.IssuedAt, committed, "authorization_issued", record)
}

func (w *Writer) WriteTraffic(ctx context.Context, record TrafficRecord, committed time.Time) error {
	return w.write(ctx, "traffic", record.ReportedAt.Format(time.RFC3339Nano), committed, "traffic_event", record)
}

func (w *Writer) WriteAuthorizationStatus(ctx context.Context, record AuthorizationStatusRecord, committed time.Time) error {
	return w.write(ctx, "authorization_status", record.OccurredAt.Format(time.RFC3339Nano),
		committed, "authorization_status", record)
}

func (w *Writer) write(ctx context.Context, kind, eventTime string, committed time.Time, eventType string, payload any) error {
	if w == nil {
		return nil
	}
	when, err := parseEventTime(eventTime)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("编码归档事件失败：%w", err)
	}
	envelope := envelope{
		SchemaVersion: schemaVersion,
		EventType:     eventType,
		EventTime:     when.Format(time.RFC3339Nano),
		DBCommitTime:  committed.UTC().Format(time.RFC3339Nano),
		EventHash:     eventHash(eventType, body),
		Payload:       json.RawMessage(body),
	}
	line, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("编码归档事件信封失败：%w", err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	path := filepath.Join(w.root, kind, when.Format("2006-01-02")+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建归档目录失败：%w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开归档文件失败：%w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("写入归档文件失败：%w", err)
	}
	return nil
}

type envelope struct {
	SchemaVersion int             `json:"schema_version"`
	EventType     string          `json:"event_type"`
	EventTime     string          `json:"event_time"`
	DBCommitTime  string          `json:"db_commit_time"`
	EventHash     string          `json:"event_hash"`
	Payload       json.RawMessage `json:"payload"`
}

func parseEventTime(value string) (time.Time, error) {
	when, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("解析归档事件时间失败：%w", err)
	}
	return when.UTC(), nil
}

func eventHash(eventType string, body []byte) string {
	sum := sha256.Sum256(append([]byte(eventType+"\n"), body...))
	return hex.EncodeToString(sum[:])
}
