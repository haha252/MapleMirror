package public

import (
	"context"
	"strings"
	"testing"
	"time"
)

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

func TestNodesTableFormatsRecentHeartbeat(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("CST", 8*60*60)
	defer func() { time.Local = old }()

	body := nodesTable([]NodeSummary{{
		PublicName:    "节点一",
		State:         "syncing",
		LastHeartbeat: "2026-05-31T04:01:00Z",
		RoutingReady:  true,
	}})
	if !strings.Contains(body, "最近心跳：2026/05/31 12:01") {
		t.Fatalf("最近心跳格式不正确：%s", body)
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

func TestAssetsUnavailableReasonExplainsNotReadyReplica(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET routing_ready = 0 WHERE id = 'node-1'`)
	mustExec(t, db, `UPDATE nodes SET public_download_base_url = '' WHERE id = 'node-1'`)
	store := Store{DB: db}

	assets, err := store.Assets(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(assets))
	}
	if assets[0].UnavailableReason != "持有副本的节点尚未同步就绪" {
		t.Fatalf("unexpected unavailable reason: %q", assets[0].UnavailableReason)
	}
	if assets[0].UnavailableDetails == "" {
		t.Fatal("expected unavailable details")
	}
}

func TestProjectsUnavailableReasonExplainsNotReadyReplica(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET routing_ready = 0 WHERE id = 'node-1'`)
	mustExec(t, db, `UPDATE nodes SET public_download_base_url = '' WHERE id = 'node-1'`)
	store := Store{DB: db}

	projects, err := store.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].UnavailableReason != "项目副本已存在，但节点尚未同步就绪" {
		t.Fatalf("unexpected project reason: %q", projects[0].UnavailableReason)
	}
	if projects[0].UnavailableDetails == "" {
		t.Fatal("expected project details")
	}
}

func TestNodesExposeRoutingReadyReason(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET routing_ready = 0 WHERE id = 'node-1'`)
	mustExec(t, db, `UPDATE node_inventory SET state = 'mismatch' WHERE node_id = 'node-1' AND asset_id = 'asset-1'`)
	mustExec(t, db, `INSERT INTO node_control_sessions
		(id, node_id, certificate_id, request_id, connected_at, last_message_sequence)
		VALUES ('sess-1', 'node-1', NULL, 'req-1', 'now', 0)`)
	mustExec(t, db, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'asset-1', 'required', 'now')`)
	store := Store{DB: db}

	nodes, err := store.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].RoutingReadyReason != "尚未上报完整库存，未完成最终对账" {
		t.Fatalf("unexpected routing-ready reason: %q", nodes[0].RoutingReadyReason)
	}
	if nodes[0].RoutingReadyDetails == "" {
		t.Fatal("expected routing-ready details")
	}
}

func TestNodesDoNotClaimDisconnectedWhenHeartbeatExists(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET routing_ready = 0, last_heartbeat_at = 'now' WHERE id = 'node-1'`)
	mustExec(t, db, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'asset-1', 'required', 'now')`)
	store := Store{DB: db}

	nodes, err := store.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].RoutingReadyReason == "控制连接当前未建立" {
		t.Fatalf("unexpected disconnected reason: %q", nodes[0].RoutingReadyReason)
	}
}
