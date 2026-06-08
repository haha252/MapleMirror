package public

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func openMaster(t *testing.T) *sql.DB {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path:        filepath.Join(t.TempDir(), "master.db"),
		BusyTimeout: "5s",
		WAL:         &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedRoutableAsset(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目一', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`)
	mustExec(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'v1', 0, 'now', 1, 'now')`)
	mustExec(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'a.zip', 'amd64', 12,
		'https://example.test/a.zip', 'sha256:aa', 'candidate', 'now')`)
	mustExec(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, last_heartbeat_at,
		routing_ready, public_download_base_url, created_at, updated_at)
		VALUES ('node-1', '节点一', 'syncing', 1, '2026-01-01T00:00:00Z',
		1, 'https://node-1.example.com', '2026-01-01T00:00:00Z', '2026-01-01T00:00:02Z')`)
	mustExec(t, db, `INSERT INTO node_control_sessions
		(id, node_id, certificate_id, request_id, connected_at, last_message_sequence)
		VALUES ('sess-seed', 'node-1', NULL, 'req-seed', '2026-01-01T00:00:00Z', 0)`)
	mustExec(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'asset-1', 'sha256:aa', 12, '2026-01-01T00:00:01Z', 'verified')`)
	mustExec(t, db, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'asset-1', 'required', 'now')`)
}

func seedAvailabilitySamples(t *testing.T, db *sql.DB, nodeID string) {
	t.Helper()
	for i := 1; i <= 3; i++ {
		start := timeNow().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339Nano)
		end := timeNow().Add(-time.Duration(i)*time.Hour + time.Minute).Format(time.RFC3339Nano)
		_, err := db.Exec(`INSERT INTO node_availability_samples
			(node_id, sample_start, sample_end, routable, heartbeat_ok)
			VALUES (?, ?, ?, 1, 1)`, nodeID, start, end)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func assertChallengeLoadable(t *testing.T, store Store, challengeID string) {
	t.Helper()
	if _, err := store.LoadChallenge(context.Background(), challengeID); err != nil {
		t.Fatalf("challenge should remain retryable: %v", err)
	}
}

func assertPublicTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count=%d want %d", table, got, want)
	}
}

func solveNonce(challenge Challenge) string {
	for i := 0; ; i++ {
		nonce := strconv.Itoa(i)
		if validLeadingZeros(challenge, nonce) {
			return nonce
		}
	}
}
