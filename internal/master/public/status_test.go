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
		DownloadReady: true,
	}})
	if !strings.Contains(body, "最近心跳：2026/05/31 12:01") {
		t.Fatalf("最近心跳格式不正确：%s", body)
	}
	if !strings.Contains(body, "下载就绪：是") {
		t.Fatalf("下载就绪标签不正确：%s", body)
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
	if assets[0].UnavailableReason != "持有副本的节点尚未下载就绪" {
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
	if projects[0].UnavailableReason != "项目副本已存在，但节点尚未下载就绪" {
		t.Fatalf("unexpected project reason: %q", projects[0].UnavailableReason)
	}
	if projects[0].UnavailableDetails == "" {
		t.Fatal("expected project details")
	}
}

func TestNodesExposeDownloadReadyIndependentlyOfRoutingReady(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET routing_ready = 0 WHERE id = 'node-1'`)
	store := Store{DB: db}

	nodes, err := store.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if !nodes[0].DownloadReady {
		t.Fatal("expected download-ready to remain true when routing_ready is false")
	}
	if nodes[0].RoutingReady {
		t.Fatal("expected routing_ready to remain false")
	}
}

func TestNodesExposeDownloadReadyReasonWhenPublicUrlMissing(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET routing_ready = 0, public_download_base_url = '' WHERE id = 'node-1'`)
	store := Store{DB: db}

	nodes, err := store.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].DownloadReady {
		t.Fatal("expected download-ready to be false when public URL is missing")
	}
	if nodes[0].DownloadReadyReason != "节点尚未提供公网下载地址" {
		t.Fatalf("unexpected download-ready reason: %q", nodes[0].DownloadReadyReason)
	}
	if nodes[0].DownloadReadyDetails == "" {
		t.Fatal("expected download-ready details")
	}
}

func TestNodesExposeDownloadReadyReasons(t *testing.T) {
	cases := []struct {
		name     string
		setupSQL []string
		reason   string
	}{
		{
			name: "no public copies",
			setupSQL: []string{
				"UPDATE nodes SET routing_ready = 0 WHERE id = 'node-1'",
				"DELETE FROM node_inventory WHERE node_id = 'node-1' AND asset_id = 'asset-1'",
			},
			reason: "暂无可公开下载的资产副本",
		},
		{
			name: "digest mismatch",
			setupSQL: []string{
				"UPDATE nodes SET routing_ready = 0 WHERE id = 'node-1'",
				"UPDATE node_inventory SET local_digest_sha256 = 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' WHERE node_id = 'node-1' AND asset_id = 'asset-1'",
			},
			reason: "副本校验未通过",
		},
		{
			name: "missing public url",
			setupSQL: []string{
				"UPDATE nodes SET routing_ready = 0, public_download_base_url = '' WHERE id = 'node-1'",
			},
			reason: "节点尚未提供公网下载地址",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openMaster(t)
			seedRoutableAsset(t, db)
			for _, stmt := range tc.setupSQL {
				mustExec(t, db, stmt)
			}
			store := Store{DB: db}

			nodes, err := store.Nodes(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(nodes) != 1 {
				t.Fatalf("expected 1 node, got %d", len(nodes))
			}
			if nodes[0].DownloadReady {
				t.Fatalf("expected download-ready to be false for %s", tc.name)
			}
			if nodes[0].DownloadReadyReason != tc.reason {
				t.Fatalf("unexpected download-ready reason: %q", nodes[0].DownloadReadyReason)
			}
			if nodes[0].DownloadReadyDetails == "" {
				t.Fatal("expected download-ready details")
			}
		})
	}
}
