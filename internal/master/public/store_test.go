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
	var tokens int64
	err = db.QueryRow(`SELECT tokens_microunits FROM quota_buckets
		WHERE scope_kind = 'ipv4_32' AND scope_key = '192.0.2.1/32'`).Scan(&tokens)
	if err != nil || tokens != 119_000_000 {
		t.Fatalf("请求额度未按倍率扣减：tokens=%d err=%v", tokens, err)
	}
	var authCount int
	err = db.QueryRow(`SELECT authorization_count FROM daily_project_stats
		WHERE project_id = 'p1'`).Scan(&authCount)
	if err != nil || authCount != 1 {
		t.Fatalf("下载授权次数未入账：count=%d err=%v", authCount, err)
	}
}

func TestIssueAuthorizationRejectsRequestQuotaExhausted(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	_, _ = db.Exec(`UPDATE projects SET download_multiplier = 3 WHERE id = 'p1'`)
	store := Store{DB: db, Quota: newQuotaPolicy(config.Quota{
		RequestBuckets: config.RequestBuckets{
			IPv432:  config.Bucket{Capacity: 1, FullRefill: "48h"},
			IPv424:  config.Bucket{Capacity: 10, FullRefill: "48h"},
			IPv6128: config.Bucket{Capacity: 1, FullRefill: "48h"},
			IPv664:  config.Bucket{Capacity: 10, FullRefill: "48h"},
		},
		DailyTraffic: config.DailyTraffic{
			IPv432: "3 GiB", IPv424: "20 GiB", IPv6128: "3 GiB", IPv664: "20 GiB",
		},
	})}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.IssueAuthorization(context.Background(), challenge, time.Minute, "req-2")
	if err != errRequestQuota {
		t.Fatalf("请求额度不足应拒绝授权：%v", err)
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

func TestNodesReadsSLAAfterClosingNodeRows(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	seedAvailabilitySamples(t, db, "node-1")
	store := Store{DB: db}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	nodes, err := store.Nodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].SLA24H != "100.00%" {
		t.Fatalf("节点 SLA 读取异常：%+v", nodes)
	}
}

func TestSampleNodeAvailabilityWritesAfterClosingNodeRows(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := store.SampleNodeAvailability(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM node_availability_samples
		WHERE node_id = 'node-1'`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("节点 SLA 采样未写入：count=%d err=%v", count, err)
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

func solveNonce(c Challenge) string {
	for i := 0; ; i++ {
		nonce := strconv.Itoa(i)
		if validLeadingZeros(c, nonce) {
			return nonce
		}
	}
}
