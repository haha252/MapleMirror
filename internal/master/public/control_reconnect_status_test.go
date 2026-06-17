package public

import (
	"context"
	"strings"
	"testing"
)

func TestNodesKeepDownloadReadyDuringControlReconnectWindow(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE node_control_sessions SET disconnected_at = '2026-01-01T00:00:10Z',
		close_reason = '控制连接超时' WHERE id = 'sess-seed'`)
	store := Store{DB: db}

	nodes, err := store.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if !nodes[0].DownloadReady {
		t.Fatalf("control reconnect window should keep download ready: %+v", nodes[0])
	}
	if nodes[0].DownloadReadyReason != "" {
		t.Fatalf("download-ready node should not expose reconnect reason: %+v", nodes[0])
	}
}

func TestNodesHideRawControlTimeoutFromPublicReason(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET state = 'offline' WHERE id = 'node-1'`)
	mustExec(t, db, `UPDATE node_control_sessions SET disconnected_at = '2026-01-01T00:00:10Z',
		close_reason = 'read tcp 10.6.0.7:10001->38.49.217.144:53280: i/o timeout'
		WHERE id = 'sess-seed'`)
	store := Store{DB: db}

	nodes, err := store.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].DownloadReady {
		t.Fatal("offline node should not be download ready")
	}
	if nodes[0].DownloadReadyReason != "控制连接超时，等待节点重连" {
		t.Fatalf("unexpected public reason: %q", nodes[0].DownloadReadyReason)
	}
	if strings.Contains(nodes[0].DownloadReadyReason+nodes[0].DownloadReadyDetails, "read tcp") {
		t.Fatalf("public reason leaked raw socket error: %+v", nodes[0])
	}
}
