package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/master/accountingarchive"
	"mirror-server/internal/protocol"
)

func TestAcceptTrafficEventArchivesNewEventsOnly(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	root := t.TempDir()
	repo.Archive = accountingarchive.New(root)
	seedTrafficAuth(t, repo)
	session := Session{ID: "sess-1", NodeID: "node-1"}
	reported := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	event := protocol.TrafficEvent{
		EventSequence: 1, AuthorizationID: "auth-1", AssetID: "asset-1",
		NodeRequestID: "node-req-1", MasterRequestID: "master-req-1",
		SentBytes: 5, Status: "completed", ReportedAt: reported,
	}
	if _, err := repo.AcceptTrafficEvent(context.Background(), session, 2, event); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AcceptTrafficEvent(context.Background(), session, 3, event); err != nil {
		t.Fatal(err)
	}
	line := readArchiveLine(t, filepath.Join(root, "traffic", "2026-05-29.jsonl"))
	envelope := decodeArchiveEnvelope(t, line)
	if envelope.EventType != "traffic_event" || envelope.EventTime != reported.Format(time.RFC3339Nano) {
		t.Fatalf("流量归档信封错误：%+v", envelope)
	}
	payload := decodeArchivePayload(t, envelope.Payload)
	if payload["authorization_id"] != "auth-1" || payload["sent_bytes"].(float64) != 5 {
		t.Fatalf("流量归档 payload 错误：%+v", payload)
	}
	statusLine := readOnlyGlobArchiveLine(t, filepath.Join(root, "authorization_status", "*.jsonl"))
	if decodeArchiveEnvelope(t, statusLine).EventType != "authorization_status" {
		t.Fatalf("首次传输应归档授权 active 状态：%s", statusLine)
	}
}

func TestAcceptAuthorizationStatusEventArchivesStatus(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()
	root := t.TempDir()
	repo.Archive = accountingarchive.New(root)
	seedAuthorizationStatusAuth(t, repo, "node-1")
	session := Session{ID: "sess-1", NodeID: "node-1"}
	repo.runtime().StartSession(session)
	occurred := time.Date(2026, 5, 29, 13, 0, 0, 0, time.UTC)
	_, err := repo.AcceptAuthorizationStatusEvent(context.Background(), session, 2,
		protocol.AuthorizationStatusEvent{AuthorizationID: "auth-1", AssetID: "asset-1",
			Status: "expired_idle", Reason: "idle", OccurredAt: occurred})
	if err != nil {
		t.Fatal(err)
	}
	line := readArchiveLine(t, filepath.Join(root, "authorization_status", "2026-05-29.jsonl"))
	envelope := decodeArchiveEnvelope(t, line)
	payload := decodeArchivePayload(t, envelope.Payload)
	if envelope.EventType != "authorization_status" || payload["status"] != "expired_idle" ||
		payload["reason"] != "idle" {
		t.Fatalf("授权状态归档错误：envelope=%+v payload=%+v", envelope, payload)
	}
}

type archiveEnvelope struct {
	EventType string          `json:"event_type"`
	EventTime string          `json:"event_time"`
	Payload   json.RawMessage `json:"payload"`
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

func readOnlyGlobArchiveLine(t *testing.T, pattern string) string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) != 1 {
		t.Fatalf("归档文件匹配错误：matches=%v err=%v", matches, err)
	}
	return readArchiveLine(t, matches[0])
}

func decodeArchiveEnvelope(t *testing.T, line string) archiveEnvelope {
	t.Helper()
	var envelope archiveEnvelope
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}

func decodeArchivePayload(t *testing.T, data json.RawMessage) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
