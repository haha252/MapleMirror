package assignment

import (
	"context"
	"database/sql"
	"testing"
)

func seedProject(t *testing.T, db *sql.DB, id, name string, downloads int) {
	t.Helper()
	mustExec(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES (?, ?, ?, 1, 1, 0, 1, 'hash', 'now')`, id, name, "owner/"+id)
	mustExec(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES (?, ?, 1, 'v1', 0, '2026-06-01T00:00:00Z', 1, 'now')`, "rel-"+id, id)
	mustExec(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES (?, ?, 1, 'demo.zip', '', 64, 'https://example.invalid/demo.zip',
		'sha256', 'candidate', 'now')`, "asset-"+id, "rel-"+id)
	mustExec(t, db, `INSERT INTO daily_project_stats
		(stat_day, project_id, authorization_count, transfer_started_count, sent_bytes)
		VALUES ('2026-06-07', ?, ?, 0, 0)`, id, downloads)
}

func reconcile(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReconcileNode(context.Background(), tx, "node-1", "2026-06-07T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func assertAssigned(t *testing.T, db *sql.DB, projectID string, want bool) {
	t.Helper()
	var assigned int
	err := db.QueryRow(`SELECT assigned FROM node_project_assignments
		WHERE node_id = 'node-1' AND project_id = ?`, projectID).Scan(&assigned)
	if err != nil {
		t.Fatal(err)
	}
	if (assigned == 1) != want {
		t.Fatalf("project %s assigned=%v want %v", projectID, assigned == 1, want)
	}
}

func assertRequiredTargets(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var got int
	err := db.QueryRow(`SELECT COUNT(*) FROM target_inventory
		WHERE node_id = 'node-1' AND desired_state = 'required'`).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("required targets=%d want %d", got, want)
	}
}

func assertTaskCount(t *testing.T, db *sql.DB, taskType, state string, want int) {
	t.Helper()
	var got int
	err := db.QueryRow(`SELECT COUNT(*) FROM node_tasks
		WHERE task_type = ? AND state = ?`, taskType, state).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s %s tasks=%d want %d", taskType, state, got, want)
	}
}

func assertTimestamp(t *testing.T, db *sql.DB, table, want, where string) {
	t.Helper()
	var got string
	err := db.QueryRow(`SELECT updated_at FROM ` + table + ` WHERE ` + where).Scan(&got)
	if err != nil || got != want {
		t.Fatalf("%s updated_at=%q want %q err=%v", table, got, want, err)
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func boolPtr(value bool) *bool { return &value }
