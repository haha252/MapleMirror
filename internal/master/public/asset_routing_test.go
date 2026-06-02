package public

import (
	"context"
	"testing"
	"time"
)

func TestFileReadyAssetAvailableWhenNodeRoutingReadyFalse(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET routing_ready = 0 WHERE id = 'node-1'`)
	store := Store{DB: db}

	projects, err := store.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || !projects[0].Available {
		t.Fatalf("当前会话内已校验的项目资产应可用：%+v", projects)
	}

	assets, err := store.Assets(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || !assets[0].Available {
		t.Fatalf("当前会话内已校验的文件应可用：%+v", assets)
	}
}

func TestIssueAuthorizationAllowsFileReadyReplicaWhenNodeRoutingReadyFalse(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET routing_ready = 0 WHERE id = 'node-1'`)
	store := Store{DB: db}

	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, debug, err := store.IssueAuthorization(context.Background(), challenge, time.Minute, "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Claims.NodeID != "node-1" || debug.DownloadURL != "https://node-1.example.com/p1/v1/a.zip" {
		t.Fatalf("授权未绑定当前文件已就绪的节点：claims=%+v debug=%+v", auth.Claims, debug)
	}
}

func TestRecentlyReconnectedNodeKeepsVerifiedReplicaRoutable(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET routing_ready = 0 WHERE id = 'node-1'`)
	mustExec(t, db, `UPDATE nodes SET last_heartbeat_at = '2026-01-01T00:00:03Z' WHERE id = 'node-1'`)
	store := Store{DB: db}

	assets, err := store.Assets(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || !assets[0].Available {
		t.Fatalf("重连心跳后的已校验库存应继续可用：%+v", assets)
	}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, _, err := store.IssueAuthorization(context.Background(), challenge, time.Minute, "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Claims.NodeID != "node-1" {
		t.Fatalf("授权应继续绑定重连节点：%+v", auth.Claims)
	}
}

func TestCreateChallengeRejectsUnroutableReplicaConditions(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{name: "offline", sql: `UPDATE nodes SET state = 'offline' WHERE id = 'node-1'`},
		{name: "disabled", sql: `UPDATE nodes SET state = 'disabled' WHERE id = 'node-1'`},
		{name: "missing public url", sql: `UPDATE nodes SET public_download_base_url = '' WHERE id = 'node-1'`},
		{name: "digest mismatch", sql: `UPDATE node_inventory SET local_digest_sha256 = 'sha256:bb'
			WHERE node_id = 'node-1' AND asset_id = 'asset-1'`},
		{name: "size mismatch", sql: `UPDATE node_inventory SET size_bytes = 13
			WHERE node_id = 'node-1' AND asset_id = 'asset-1'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openMaster(t)
			seedRoutableAsset(t, db)
			mustExec(t, db, tc.sql)
			store := Store{DB: db}

			_, err := store.CreateChallenge(context.Background(), "api_pow",
				"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
			if err == nil {
				t.Fatal("不可路由副本不应创建挑战")
			}
		})
	}
}
