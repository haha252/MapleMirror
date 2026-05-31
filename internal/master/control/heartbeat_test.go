package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestHeartbeatKeepsNodeNonRoutable(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	ctx := context.Background()
	session := seedNodeAndSession(t, repo)
	_, err := repo.AcceptHeartbeat(ctx, session, 1, protocol.Heartbeat{
		Status: "syncing", FreeBytes: 100, PublicDownloadBaseURL: "https://node-1.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	var ready int
	var state string
	err = repo.DB.QueryRow("SELECT routing_ready, state FROM nodes WHERE id = ?", session.NodeID).Scan(&ready, &state)
	if err != nil || ready != 0 || state != "syncing" {
		t.Fatalf("心跳不得使节点可路由，ready=%d state=%s err=%v", ready, state, err)
	}
	var downloadURL string
	if err := repo.DB.QueryRow("SELECT public_download_base_url FROM nodes WHERE id = ?", session.NodeID).Scan(&downloadURL); err != nil || downloadURL != "https://node-1.example.com" {
		t.Fatalf("节点公网下载地址未写入：url=%q err=%v", downloadURL, err)
	}
}

func TestHeartbeatWithInvalidPublicDownloadURLKeepsControlAlive(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	_, err := repo.DB.Exec(`UPDATE nodes SET public_download_base_url = ?
		WHERE id = ?`, "https://old.example.com", session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{
		Status: "syncing", FreeBytes: 100, PublicDownloadBaseURL: "not-a-public-url",
	})
	if err != nil {
		t.Fatal(err)
	}
	var state, downloadURL string
	err = repo.DB.QueryRow(`SELECT state, public_download_base_url FROM nodes
		WHERE id = ?`, session.NodeID).Scan(&state, &downloadURL)
	if err != nil {
		t.Fatal(err)
	}
	if state != "syncing" || downloadURL != "" {
		t.Fatalf("无效公网地址应只清空路由地址并保持连接，state=%q url=%q", state, downloadURL)
	}
}

func TestMarkOfflineMasksRouting(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	_, err := repo.DB.Exec(`UPDATE nodes SET last_heartbeat_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano), session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := repo.MarkOffline(context.Background(), time.Second)
	if err != nil || changed == 0 {
		t.Fatalf("离线屏蔽未生效：changed=%d err=%v", changed, err)
	}
	var ready int
	_ = repo.DB.QueryRow("SELECT routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&ready)
	if ready != 0 {
		t.Fatal("离线节点必须保持不可路由")
	}
}

func TestMarkOfflineDoesNotRepeatAlreadyOfflineNode(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	_, err := repo.DB.Exec(`UPDATE nodes SET state = 'offline', last_heartbeat_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano), session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := repo.MarkOffline(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("已离线节点不应重复计数，changed=%d", changed)
	}
}

func seedNodeAndSession(t *testing.T, repo Repository) Session {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := repo.DB.Exec(`INSERT INTO nodes
		(id, public_name, certificate_fingerprint, state, target_bandwidth_bps,
		routing_ready, created_at, updated_at) VALUES
		('node-1', '节点一', 'sha256:aa', 'syncing', 0, 0, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB.Exec(`INSERT INTO node_certificates
		(id, node_id, serial_number, fingerprint, not_before, not_after,
		status, issued_request_id, created_at) VALUES
		('cert-1', 'node-1', '1', 'sha256:aa', ?, ?, 'active', 'req', ?)`,
		now, time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.StartSession(context.Background(), "sha256:aa", "req-session")
	if err != nil {
		t.Fatal(err)
	}
	return session
}
