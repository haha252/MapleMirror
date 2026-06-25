package accountingarchive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriterWritesDailyJSONL(t *testing.T) {
	root := t.TempDir()
	writer := New(root)
	reported := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	err := writer.WriteTraffic(context.Background(), TrafficRecord{
		NodeID: "node-1", EventSequence: 7, AuthorizationID: "auth-1",
		AssetID: "asset-1", NodeRequestID: "node-req", MasterRequestID: "master-req",
		SentBytes: 12, Status: "completed", ReportedAt: reported,
		AccountedAt: reported.Add(time.Second).Format(time.RFC3339Nano),
	}, reported.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	line := readArchiveLine(t, filepath.Join(root, "traffic", "2026-05-29.jsonl"))
	var envelope struct {
		SchemaVersion int             `json:"schema_version"`
		EventType     string          `json:"event_type"`
		EventTime     string          `json:"event_time"`
		DBCommitTime  string          `json:"db_commit_time"`
		EventHash     string          `json:"event_hash"`
		Payload       json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion != 1 || envelope.EventType != "traffic_event" ||
		envelope.EventTime != reported.Format(time.RFC3339Nano) ||
		envelope.DBCommitTime == "" || envelope.EventHash == "" {
		t.Fatalf("归档信封字段不完整：%+v", envelope)
	}
	var payload TrafficRecord
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.AuthorizationID != "auth-1" || payload.SentBytes != 12 {
		t.Fatalf("归档 payload 错误：%+v", payload)
	}
}

func readArchiveLine(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("归档行数=%d want 1: %s", len(lines), data)
	}
	return lines[0]
}
