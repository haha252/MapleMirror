package storage

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"

	_ "modernc.org/sqlite"
)

func TestOpenMasterLogsVersionUpgradeSteps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('master', 3, '` + now + `')`,
		`CREATE TABLE nodes (
			id TEXT PRIMARY KEY,
			public_name TEXT NOT NULL,
			certificate_fingerprint TEXT,
			state TEXT NOT NULL,
			target_bandwidth_bps INTEGER NOT NULL,
			last_heartbeat_at TEXT,
			routing_ready INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var events []versionLogEvent
	opened, err := OpenMaster(config.Database{Path: path, BusyTimeout: "5s"},
		WithVersionLogger(collectVersionLogEvents(&events)))
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()

	want := []versionLogEvent{
		{Message: "数据库需要升级", Kind: "master", Current: 3, Target: 8},
		{Message: "数据库升级器开始执行", Kind: "master", From: 3, To: 4},
		{Message: "数据库升级器执行完成", Kind: "master", From: 3, To: 4},
		{Message: "数据库升级器开始执行", Kind: "master", From: 4, To: 5},
		{Message: "数据库升级器执行完成", Kind: "master", From: 4, To: 5},
		{Message: "数据库升级器开始执行", Kind: "master", From: 5, To: 6},
		{Message: "数据库升级器执行完成", Kind: "master", From: 5, To: 6},
		{Message: "数据库升级器开始执行", Kind: "master", From: 6, To: 7},
		{Message: "数据库升级器执行完成", Kind: "master", From: 6, To: 7},
		{Message: "数据库升级器开始执行", Kind: "master", From: 7, To: 8},
		{Message: "数据库升级器执行完成", Kind: "master", From: 7, To: 8},
	}
	assertVersionLogEvents(t, events, want)
}

func TestOpenMasterDoesNotLogUpgradeStepsWhenCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	opened, err := OpenMaster(config.Database{Path: path, BusyTimeout: "5s"})
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}

	var events []versionLogEvent
	reopened, err := OpenMaster(config.Database{Path: path, BusyTimeout: "5s"},
		WithVersionLogger(collectVersionLogEvents(&events)))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if len(events) != 0 {
		t.Fatalf("expected no version upgrade logs, got %+v", events)
	}
}

type versionLogEvent struct {
	Message string
	Kind    string
	Current int
	Target  int
	From    int
	To      int
	Mode    string
}

func collectVersionLogEvents(events *[]versionLogEvent) versionLogFunc {
	return func(_ context.Context, message string, attrs ...slog.Attr) {
		event := versionLogEvent{Message: message}
		for _, attr := range attrs {
			switch attr.Key {
			case "database_kind":
				event.Kind = attr.Value.String()
			case "current_version":
				event.Current = int(attr.Value.Int64())
			case "target_version":
				event.Target = int(attr.Value.Int64())
			case "from_version":
				event.From = int(attr.Value.Int64())
			case "to_version":
				event.To = int(attr.Value.Int64())
			case "checkpoint_mode":
				event.Mode = attr.Value.String()
			}
		}
		*events = append(*events, event)
	}
}

func hasCheckpointMode(events []versionLogEvent, mode string) bool {
	for _, event := range events {
		if event.Mode == mode {
			return true
		}
	}
	return false
}

func assertVersionLogEvents(t *testing.T, got, want []versionLogEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("log event count=%d want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("log event %d=%+v want %+v", i, got[i], want[i])
		}
	}
}
