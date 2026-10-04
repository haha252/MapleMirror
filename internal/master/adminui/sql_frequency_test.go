package adminui

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestSessionLastSeenThrottlesWritesWithoutCachingAuthentication(t *testing.T) {
	server, db := newTestServer(t)
	ctx := context.Background()
	token, _, err := server.store.createSession(ctx, "admin", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	mustExecAdminUI(t, db, `CREATE TABLE touch_count (count INTEGER NOT NULL)`)
	mustExecAdminUI(t, db, `INSERT INTO touch_count VALUES (0)`)
	mustExecAdminUI(t, db, `CREATE TRIGGER count_session_touch AFTER UPDATE OF last_seen_at ON admin_web_sessions
		BEGIN UPDATE touch_count SET count=count+1; END`)
	key := server.store.sessionKey(token)
	mustExecAdminUI(t, db, `UPDATE admin_web_sessions SET last_seen_at=? WHERE id=?`,
		time.Now().Add(-2*time.Minute).UTC().Format(time.RFC3339Nano), key)
	mustExecAdminUI(t, db, `UPDATE touch_count SET count=0`)
	for range 50 {
		username, ok, err := server.store.verifySession(ctx, token, "127.0.0.1")
		if err != nil || !ok || username != "admin" {
			t.Fatalf("username=%s ok=%v err=%v", username, ok, err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count FROM touch_count`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("session touch writes=%d want=1 err=%v", count, err)
	}
	if _, ok, err := server.store.verifySession(ctx, token, "127.0.0.2"); err == nil || ok {
		t.Fatal("IP mismatch must still fail immediately")
	}
	mustExecAdminUI(t, db, `UPDATE admin_web_sessions SET expires_at=? WHERE id=?`,
		time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), key)
	if _, ok, err := server.store.verifySession(ctx, token, "127.0.0.1"); err != nil || ok {
		t.Fatalf("expired session accepted: ok=%v err=%v", ok, err)
	}
	server.store.deleteSession(ctx, token)
	if _, ok, err := server.store.verifySession(ctx, token, "127.0.0.1"); err != nil || ok {
		t.Fatalf("deleted session accepted: ok=%v err=%v", ok, err)
	}
}

func TestSLAWindowsShareOnePassAndPreserveWindowTotals(t *testing.T) {
	_, db := newTestServer(t)
	now := time.Now().UTC()
	for _, id := range []string{"node-1", "node-2"} {
		mustExecAdminUI(t, db, `INSERT INTO nodes
			(id,public_name,state,target_bandwidth_bps,routing_ready,created_at,updated_at)
			VALUES (?,?,'online',0,0,'now','now')`, id, id)
	}
	for _, row := range []struct {
		hours     int
		total, ok int64
	}{{1, 10, 8}, {48, 20, 10}, {240, 30, 20}, {744, 999, 999}} {
		mustExecAdminUI(t, db, `INSERT INTO node_availability_rollups VALUES ('node-1',?,5,?,?,'now')`,
			now.Add(-time.Duration(row.hours)*time.Hour).Format(time.RFC3339Nano), row.total, row.ok)
	}
	mustExecAdminUI(t, db, `INSERT INTO node_availability_rollups VALUES ('node-2',?,5,999,999,'now')`, now.Format(time.RFC3339Nano))
	items, err := slaWindows(context.Background(), db, "node-1", now)
	if err != nil {
		t.Fatal(err)
	}
	for i, total := range []int64{10, 30, 60} {
		want := []float64{.8, .6, 38.0 / 60}[i]
		if items[i]["sample_count"] != total || math.Abs(items[i]["availability_ratio"].(float64)-want) > 1e-9 {
			t.Fatalf("window=%v", items[i])
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := slaWindows(ctx, db, "node-1", now); err == nil {
		t.Fatal("database cancellation must not appear as empty SLA samples")
	}
}
