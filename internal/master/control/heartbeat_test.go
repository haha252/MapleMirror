package control

import (
	"context"
	"encoding/json"
	"strings"
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
	if err != nil || ready != 0 || state != "online" {
		t.Fatalf("心跳不得使节点可路由，ready=%d state=%s err=%v", ready, state, err)
	}
	var downloadURL string
	if err := repo.DB.QueryRow("SELECT public_download_base_url FROM nodes WHERE id = ?", session.NodeID).Scan(&downloadURL); err != nil || downloadURL != "https://node-1.example.com" {
		t.Fatalf("节点公网下载地址未写入：url=%q err=%v", downloadURL, err)
	}
	var heartbeatRows int
	_ = repo.DB.QueryRow("SELECT COUNT(*) FROM node_heartbeats").Scan(&heartbeatRows)
	if heartbeatRows != 0 {
		t.Fatalf("心跳摘要不应写入 SQLite 历史表，rows=%d", heartbeatRows)
	}
	latest, err := repo.LatestHeartbeat(ctx, session.NodeID)
	if err != nil || latest["free_bytes"] != int64(100) {
		t.Fatalf("runtime 心跳摘要不符合预期 latest=%v err=%v", latest, err)
	}
}

func TestHeartbeatUpdatesMaxMirrorProjects(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)

	_, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{
		Status: "syncing", MaxMirrorProjects: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got int
	if err := repo.DB.QueryRow(`SELECT max_mirror_projects FROM nodes
		WHERE id = ?`, session.NodeID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("max_mirror_projects=%d want 3", got)
	}
}

func TestHeartbeatGeneratesMissingDownloadTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-1', '', 0, 'old', 'missing')`, session.NodeID)

	_, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{
		Status: "syncing",
	})
	if err != nil {
		t.Fatal(err)
	}
	var tasks int
	err = repo.DB.QueryRow(`SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = 'asset-1' AND task_type = 'asset_download'
		AND state = 'pending'`, session.NodeID).Scan(&tasks)
	if err != nil || tasks != 1 {
		t.Fatalf("heartbeat should refill missing download task, tasks=%d err=%v", tasks, err)
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
	if state != "online" || downloadURL != "" {
		t.Fatalf("无效公网地址应只清空路由地址并保持连接，state=%q url=%q", state, downloadURL)
	}
}

func TestMarkOfflineMasksRouting(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	repo.runtime().CloseSession(session.ID)
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

func TestMarkOfflineSkipsNodeWithActiveControlSession(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	_, err := repo.DB.Exec(`UPDATE nodes SET routing_ready = 1, last_heartbeat_at = ?
		WHERE id = ?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano), session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repo.SweepOffline(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.OfflineNodes != 0 || result.ActiveDelayedNodes != 1 {
		t.Fatalf("活动会话节点不应离线：result=%+v", result)
	}
	var ready int
	var state string
	_ = repo.DB.QueryRow("SELECT routing_ready, state FROM nodes WHERE id = ?", session.NodeID).Scan(&ready, &state)
	if ready != 1 || state != "online" {
		t.Fatalf("活动会话节点不应清空同步就绪 ready=%d state=%s", ready, state)
	}
}

func TestHeartbeatDoesNotDemoteRoutingReadyToSyncingState(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	_, err := repo.DB.Exec(`UPDATE nodes SET routing_ready = 1 WHERE id = ?`, session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{
		Status: "syncing", PublicDownloadBaseURL: "https://node-1.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	var ready int
	var state string
	_ = repo.DB.QueryRow(`SELECT routing_ready, state FROM nodes WHERE id = ?`, session.NodeID).
		Scan(&ready, &state)
	if ready != 1 || state != "online" || !result.RoutingReady || result.ManagedState != "ready" {
		t.Fatalf("心跳不应把全量就绪节点退回同步业务态 ready=%d state=%s result=%+v",
			ready, state, result)
	}
}

func TestHeartbeatAckUsesActualRoutingReady(t *testing.T) {
	body := HeartbeatAck(HeartbeatResult{AcceptedSequence: 7, ManagedState: "syncing", RoutingReady: true})
	if !json.Valid(body) {
		t.Fatalf("ack json invalid: %s", string(body))
	}
	if !strings.Contains(string(body), `"routing_ready":true`) {
		t.Fatalf("ack should expose actual routing_ready: %s", string(body))
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
