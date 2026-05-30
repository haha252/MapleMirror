package public

import (
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
		routing_ready, created_at, updated_at)
		VALUES ('node-1', '节点一', 'syncing', 1, 'now', 1, 'now', 'now')`)
	mustExec(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'asset-1', 'sha256:aa', 12, 'now', 'verified')`)
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

func solveNonce(challenge Challenge) string {
	for i := 0; ; i++ {
		nonce := strconv.Itoa(i)
		if validLeadingZeros(challenge, nonce) {
			return nonce
		}
	}
}
