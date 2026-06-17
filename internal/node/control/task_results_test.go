package control

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestClientRunOnceReportsPendingTaskResultBeforeReadingNewTask(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at)
		VALUES ('task-1', 'asset-1', 'succeeded', 'sha256:abc', 12, 'ok', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	dialer := newPipeDialer(t, func(conn net.Conn) {
		defer conn.Close()
		hello, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || hello.MessageType != protocol.TypeHello {
			return
		}
		welcomeBody, _ := json.Marshal(protocol.Welcome{
			HeartbeatIntervalSecond: 10,
			HeartbeatTimeoutSecond:  30,
			ManagedState:            "syncing",
			RoutingReady:            false,
		})
		_ = protocol.WriteFrame(conn, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "welcome",
			MessageType:     protocol.TypeWelcome,
			SentAt:          time.Now().UTC(),
			NodeID:          hello.NodeID,
			RequestID:       hello.RequestID,
			ReplyTo:         hello.MessageID,
			Payload:         welcomeBody,
		})
		hb, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || hb.MessageType != protocol.TypeHeartbeat {
			return
		}
		sendAck(conn, hb)
		pressure, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || pressure.MessageType != protocol.TypePressureReport {
			return
		}
		sendAck(conn, pressure)
		result, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || result.MessageType != protocol.TypeSyncTaskResult {
			return
		}
		sendAck(conn, result)
		report, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || report.MessageType != protocol.TypeInventoryReport {
			return
		}
		sendAck(conn, report)
	})
	client := &Client{
		NodeID:         "node-1",
		Address:        "master.test:9443",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: dialer,
		DB:             db,
	}
	if _, err := client.RunOnce(); !runOnceEndedByPeer(err) {
		t.Fatal(err)
	}
	var reportedAt string
	if err := db.QueryRow(`SELECT COALESCE(reported_at, '') FROM pending_sync_task_results
		WHERE task_id = 'task-1'`).Scan(&reportedAt); err != nil {
		t.Fatal(err)
	}
	if reportedAt == "" {
		t.Fatal("待上报同步结果未标记为已上报")
	}
}

func TestStorePendingTaskResultStopsRunningAck(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-1', 'asset-1', 'asset_download', 'running', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	client := Client{NodeID: "node-1", DB: db}
	err = client.storePendingTaskResult(protocol.SyncTaskResult{
		TaskID:  "task-1",
		AssetID: "asset-1",
		Result:  "temporary_error",
		Message: "下载资产失败",
	})
	if err != nil {
		t.Fatal(err)
	}
	var state string
	err = db.QueryRow(`SELECT state FROM local_sync_tasks WHERE task_id = 'task-1'`).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	if state == "running" {
		t.Fatal("终态结果入库后不得继续保留 running 续报状态")
	}
	var running int
	err = db.QueryRow(`SELECT COUNT(*) FROM local_sync_tasks
		WHERE task_id = 'task-1' AND state = 'running'`).Scan(&running)
	if err != nil || running != 0 {
		t.Fatalf("失败任务不应再被 running ACK 查询到 running=%d err=%v", running, err)
	}
	next, err := client.sendRunningTaskAcks(nil, "req-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if next != 7 {
		t.Fatalf("终态任务不得再发送 running ACK，next=%d", next)
	}
}

func TestInventoryReconcileResultKeepsExistingForceRequest(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-reconcile', NULL, 'inventory_reconcile', 'running', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO inventory_report_cursor
		(id, next_revision, last_acked_revision, updated_at, force_report_requested_at)
		VALUES (1, 7, 6, ?, 'force-existing')`, now)
	if err != nil {
		t.Fatal(err)
	}

	client := Client{NodeID: "node-1", DB: db}
	err = client.storePendingTaskResult(protocol.SyncTaskResult{
		TaskID: "task-reconcile",
		Result: "succeeded",
	})
	if err != nil {
		t.Fatal(err)
	}

	var forceRequestedAt string
	err = db.QueryRow(`SELECT COALESCE(force_report_requested_at, '')
		FROM inventory_report_cursor WHERE id = 1`).Scan(&forceRequestedAt)
	if err != nil {
		t.Fatal(err)
	}
	if forceRequestedAt != "force-existing" {
		t.Fatalf("inventory reconcile result refreshed existing force marker: %q", forceRequestedAt)
	}
}

func TestStorePendingTaskResultRetriesLockedDatabase(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-1', 'asset-1', 'asset_download', 'running', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	lockTx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockTx.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, created_at)
		VALUES ('lock-holder', 'asset-lock', 'temporary_error', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = lockTx.Rollback()
		close(released)
	}()
	client := Client{NodeID: "node-1", DB: db}
	err = client.storePendingTaskResultWithRetry(protocol.SyncTaskResult{
		TaskID:  "task-1",
		AssetID: "asset-1",
		Result:  "succeeded",
		Message: "ok",
	})
	<-released
	if err != nil {
		t.Fatal(err)
	}
	var result string
	if err := db.QueryRow(`SELECT result FROM pending_sync_task_results
		WHERE task_id = 'task-1'`).Scan(&result); err != nil {
		t.Fatal(err)
	}
	if result != "succeeded" {
		t.Fatalf("stored result=%s", result)
	}
}

func TestRunOnceClearsInterruptedLocalRunningTasks(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-1', 'asset-1', 'asset_download', 'running', ?)`,
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	client := Client{NodeID: "node-1", DB: db}
	if err := client.resetInterruptedLocalTasks(); err != nil {
		t.Fatal(err)
	}
	var state string
	err = db.QueryRow(`SELECT state FROM local_sync_tasks WHERE task_id = 'task-1'`).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	if state != "interrupted" {
		t.Fatalf("控制连接重新建立前应清理本地 running 状态，got=%s", state)
	}
	var result string
	err = db.QueryRow(`SELECT result FROM pending_sync_task_results
		WHERE task_id = 'task-1'`).Scan(&result)
	if err != nil || result != "temporary_error" {
		t.Fatalf("interrupted task should enqueue temporary_error, result=%q err=%v", result, err)
	}
}

func TestRecoverInterruptedLocalTasksOnlyRunsOncePerClient(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-1', 'asset-1', 'asset_download', 'running', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{NodeID: "node-1", DB: db}
	if err := client.recoverInterruptedLocalTasksOnce(); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-2', 'asset-2', 'asset_download', 'running', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.recoverInterruptedLocalTasksOnce(); err != nil {
		t.Fatal(err)
	}
	var state string
	err = db.QueryRow(`SELECT state FROM local_sync_tasks WHERE task_id = 'task-2'`).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	if state != "running" {
		t.Fatalf("same-process reconnect must not interrupt active task, got=%s", state)
	}
	var pending int
	err = db.QueryRow(`SELECT COUNT(*) FROM pending_sync_task_results
		WHERE task_id = 'task-2'`).Scan(&pending)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("same-process reconnect queued %d false interruption result(s)", pending)
	}
}

func TestShouldLogRunningTaskAckRateLimitsPerTask(t *testing.T) {
	client := Client{
		runningTaskAckLogged: map[string]time.Time{},
	}
	if !client.shouldLogRunningTaskAck("task-1") {
		t.Fatal("first running ack should be logged")
	}
	if client.shouldLogRunningTaskAck("task-1") {
		t.Fatal("duplicate running ack within window should not be logged")
	}
	client.runningTaskAckLogged["task-1"] = time.Now().Add(-runningTaskAckLogInterval - time.Second)
	if !client.shouldLogRunningTaskAck("task-1") {
		t.Fatal("running ack should be logged again after window expires")
	}
	if !client.shouldLogRunningTaskAck("task-2") {
		t.Fatal("different task should have independent log window")
	}
}

func TestSendNextRunningTaskAckRotatesAndRateLimits(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC()
	for idx, taskID := range []string{"task-1", "task-2"} {
		_, err := db.Exec(`INSERT INTO local_sync_tasks
			(task_id, asset_id, task_type, state, updated_at)
			VALUES (?, ?, 'asset_download', 'running', ?)`,
			taskID, "asset-"+taskID, now.Add(time.Duration(idx)*time.Second).Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
	}
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	seen := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2; i++ {
			msg, ok := expectType(t, server, protocol.TypeSyncTaskAck)
			if !ok {
				return
			}
			var ack protocol.SyncTaskAck
			if err := json.Unmarshal(msg.Payload, &ack); err != nil {
				t.Error(err)
				return
			}
			seen <- ack.TaskID
			sendAck(server, msg)
		}
	}()
	ctl := Client{
		NodeID:             "node-1",
		DB:                 db,
		runningTaskAckSent: map[string]time.Time{},
	}
	next, sent, err := ctl.sendNextRunningTaskAck(clientConn, "req-1", 7)
	if err != nil || !sent || next != 8 {
		t.Fatalf("first running ack next=%d sent=%v err=%v", next, sent, err)
	}
	next, sent, err = ctl.sendNextRunningTaskAck(clientConn, "req-1", next)
	if err != nil || !sent || next != 9 {
		t.Fatalf("second running ack next=%d sent=%v err=%v", next, sent, err)
	}
	next, sent, err = ctl.sendNextRunningTaskAck(clientConn, "req-1", next)
	if err != nil || sent || next != 9 {
		t.Fatalf("recent running acks should yield next=%d sent=%v err=%v", next, sent, err)
	}
	<-done
	first := <-seen
	second := <-seen
	if first != "task-1" || second != "task-2" {
		t.Fatalf("running ack order = %s, %s", first, second)
	}
}

func TestSendNextRunningTaskAckSkipsRecentWindowAndReachesLaterTasks(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC()
	for i := 1; i <= 51; i++ {
		taskID := fmt.Sprintf("task-%02d", i)
		_, err := db.Exec(`INSERT INTO local_sync_tasks
			(task_id, asset_id, task_type, state, updated_at)
			VALUES (?, ?, 'asset_download', 'running', ?)`,
			taskID, "asset-"+taskID, now.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
	}
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, ok := expectType(t, server, protocol.TypeSyncTaskAck)
		if !ok {
			return
		}
		var ack protocol.SyncTaskAck
		if err := json.Unmarshal(msg.Payload, &ack); err != nil {
			t.Error(err)
			return
		}
		if ack.TaskID != "task-51" {
			t.Fatalf("expected task-51 ack, got %s", ack.TaskID)
		}
		sendAck(server, msg)
	}()
	sentAt := now.Add(-runningTaskAckLogInterval / 2)
	sent := map[string]time.Time{}
	for i := 1; i <= 50; i++ {
		sent[fmt.Sprintf("task-%02d", i)] = sentAt
	}
	ctl := Client{
		NodeID:             "node-1",
		DB:                 db,
		runningTaskAckSent: sent,
	}
	next, sentAck, err := ctl.sendNextRunningTaskAck(clientConn, "req-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if !sentAck {
		t.Fatal("expected later running task to be acknowledged")
	}
	if next != 8 {
		t.Fatalf("next sequence = %d, want 8", next)
	}
	<-done
}
