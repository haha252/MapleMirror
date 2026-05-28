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

func TestIssueAuthorizationConsumesChallengeAndBindsRoutableNode(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	nonce := solveNonce(challenge)
	if !validLeadingZeros(challenge, nonce) {
		t.Fatal("测试 nonce 未满足 PoW")
	}
	auth, err := store.IssueAuthorization(context.Background(), challenge, time.Minute, "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Claims.AssetID != "asset-1" || auth.Claims.NodeID != "node-1" {
		t.Fatalf("授权绑定错误：%+v", auth.Claims)
	}
	if _, err := store.LoadChallenge(context.Background(), challenge.ID); err == nil {
		t.Fatal("挑战提交后仍可重复使用")
	}
}

func TestCreateChallengeRejectsUnavailableAsset(t *testing.T) {
	db := openMaster(t)
	store := Store{DB: db}
	_, err := store.CreateChallenge(context.Background(), "api_pow",
		"missing", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err == nil {
		t.Fatal("不存在可路由副本时不应创建挑战")
	}
}

func openMaster(t *testing.T) *sql.DB {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
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

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func solveNonce(c Challenge) string {
	for i := 0; ; i++ {
		nonce := strconv.Itoa(i)
		if validLeadingZeros(c, nonce) {
			return nonce
		}
	}
}
