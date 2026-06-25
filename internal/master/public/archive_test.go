package public

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/master/accountingarchive"
)

func TestIssueAuthorizationArchivesIssuedEvent(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	root := t.TempDir()
	store := Store{DB: db, Archive: accountingarchive.New(root)}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, _, err := store.IssueAuthorization(context.Background(), challenge,
		testTokenLifetime(time.Minute), "req-2")
	if err != nil {
		t.Fatal(err)
	}
	line := readOnlyArchiveLine(t, filepath.Join(root, "authorization", "*.jsonl"))
	envelope := decodeArchiveEnvelope(t, line)
	payload := decodeArchivePayload(t, envelope.Payload)
	if envelope.EventType != "authorization_issued" ||
		payload["authorization_id"] != auth.Claims.AuthorizationID ||
		payload["asset_id"] != "asset-1" || payload["node_id"] != "node-1" {
		t.Fatalf("授权签发归档错误：envelope=%+v payload=%+v", envelope, payload)
	}
}

type archiveEnvelope struct {
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

func readOnlyArchiveLine(t *testing.T, pattern string) string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) != 1 {
		t.Fatalf("归档文件匹配错误：matches=%v err=%v", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("归档行数=%d want 1: %s", len(lines), data)
	}
	return lines[0]
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
