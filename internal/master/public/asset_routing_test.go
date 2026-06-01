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
	if auth.Claims.NodeID != "node-1" || debug.DownloadURL != "https://node-1.example.com/downloads/asset-1" {
		t.Fatalf("授权未绑定当前文件已就绪的节点：claims=%+v debug=%+v", auth.Claims, debug)
	}
}

func TestStaleVerifiedReplicaUnavailableUntilCurrentHeartbeatConfirmsIt(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET routing_ready = 0 WHERE id = 'node-1'`)
	mustExec(t, db, `UPDATE nodes SET last_heartbeat_at = '2026-01-01T00:00:03Z' WHERE id = 'node-1'`)
	store := Store{DB: db}

	assets, err := store.Assets(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].Available {
		t.Fatalf("最近心跳前的旧库存不应直接可用：%+v", assets)
	}
	if _, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1"); err == nil {
		t.Fatal("最近心跳前的旧库存不应签发挑战")
	}

	mustExec(t, db, `UPDATE node_inventory SET verified_at = '2026-01-01T00:00:04Z'
		WHERE node_id = 'node-1' AND asset_id = 'asset-1'`)
	assets, err = store.Assets(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || !assets[0].Available {
		t.Fatalf("当前会话重新确认后的库存应恢复可用：%+v", assets)
	}
}
