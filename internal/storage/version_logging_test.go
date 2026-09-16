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
		{Message: "数据库需要升级", Kind: "master", Current: 3, Target: masterDBVersion},
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
		{Message: "数据库升级器开始执行", Kind: "master", From: 8, To: 9},
		{Message: "数据库升级器执行完成", Kind: "master", From: 8, To: 9},
		{Message: "数据库升级器开始执行", Kind: "master", From: 9, To: 10},
		{Message: "数据库升级器执行完成", Kind: "master", From: 9, To: 10},
		{Message: "数据库升级器开始执行", Kind: "master", From: 10, To: 11},
		{Message: "数据库升级器执行完成", Kind: "master", From: 10, To: 11},
		{Message: "数据库升级器开始执行", Kind: "master", From: 11, To: 12},
		{Message: "数据库升级器执行完成", Kind: "master", From: 11, To: 12},
		{Message: "数据库升级器开始执行", Kind: "master", From: 12, To: 13},
		{Message: "数据库升级器执行完成", Kind: "master", From: 12, To: 13},
		{Message: "数据库升级器开始执行", Kind: "master", From: 13, To: 14},
		{Message: "数据库升级器执行完成", Kind: "master", From: 13, To: 14},
		{Message: "数据库升级器开始执行", Kind: "master", From: 14, To: 15},
		{Message: "数据库升级器执行完成", Kind: "master", From: 14, To: 15},
		{Message: "数据库升级器开始执行", Kind: "master", From: 15, To: 16},
		{Message: "数据库升级器执行完成", Kind: "master", From: 15, To: 16},
		{Message: "数据库升级器开始执行", Kind: "master", From: 16, To: 17},
		{Message: "数据库升级器执行完成", Kind: "master", From: 16, To: 17},
		{Message: "数据库升级器开始执行", Kind: "master", From: 17, To: 18},
		{Message: "数据库升级器执行完成", Kind: "master", From: 17, To: 18},
		{Message: "数据库升级器开始执行", Kind: "master", From: 18, To: 19},
		{Message: "数据库升级器执行完成", Kind: "master", From: 18, To: 19},
		{Message: "数据库升级器开始执行", Kind: "master", From: 19, To: 20},
		{Message: "数据库升级器执行完成", Kind: "master", From: 19, To: 20},
		{Message: "数据库升级器开始执行", Kind: "master", From: 20, To: 21},
		{Message: "数据库升级器执行完成", Kind: "master", From: 20, To: 21},
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
	Message       string
	Kind          string
	Current       int
	Target        int
	From          int
	To            int
	Mode          string
	LogFrames     int
	CheckedFrames int
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
			case "log_frames":
				event.LogFrames = int(attr.Value.Int64())
			case "checked_frames":
				event.CheckedFrames = int(attr.Value.Int64())
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

func hasVersionLogMessage(events []versionLogEvent, message string) bool {
	for _, event := range events {
		if event.Message == message {
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
