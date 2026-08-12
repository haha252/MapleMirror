package public

import (
	"context"
	"strings"
	"testing"
)

func TestNodesExposeDownloadReadyReasonWhenPublicProbeBlocked(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET public_probe_network_failures = 5,
		last_public_probe_result = 'network_error',
		last_public_probe_error = 'read tcp 192.0.2.10:10001->198.51.100.20:35290: i/o timeout'
		WHERE id = 'node-1'`)
	store := Store{DB: db, PublicProbeNetworkFailures: 5}

	nodes, err := store.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].State == "offline" {
		t.Fatal("公网探测网络失败不应把控制状态标记为离线")
	}
	if nodes[0].DownloadReady {
		t.Fatal("公网探测失败达到阈值时不应下载就绪")
	}
	if nodes[0].DownloadReadyReason != "节点在线，但公网下载入口探测失败" {
		t.Fatalf("unexpected download-ready reason: %q", nodes[0].DownloadReadyReason)
	}
	if !strings.Contains(nodes[0].DownloadReadyDetails, "i/o timeout") {
		t.Fatalf("expected probe error in details: %q", nodes[0].DownloadReadyDetails)
	}
}
