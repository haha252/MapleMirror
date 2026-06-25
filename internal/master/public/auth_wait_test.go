package public

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
	mastercontrol "mirror-server/internal/master/control"
)

func TestIssueAuthorizationWaitsForRouteRecovery(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `UPDATE nodes SET public_download_base_url = '' WHERE id = 'node-1'`)

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(time.Second)
		_, _ = db.Exec(`UPDATE nodes SET public_download_base_url = 'https://node-1.example.com'
			WHERE id = 'node-1'`)
	}()

	started := time.Now()
	auth, debug, err := store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2")
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if time.Since(started) < time.Second {
		t.Fatal("授权应等待路由恢复后再签发")
	}
	if auth.Claims.NodeID != "node-1" || debug.DownloadURL != "https://node-1.example.com/p1/v1/a.zip" {
		t.Fatalf("授权未绑定恢复后的节点：claims=%+v debug=%+v", auth.Claims, debug)
	}
}

func TestIssueAuthorizationReturnsNoRowsAfterRouteRecoveryTimeout(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `UPDATE nodes SET public_download_base_url = '' WHERE id = 'node-1'`)

	started := time.Now()
	_, _, err = store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2")
	if err != sql.ErrNoRows {
		t.Fatalf("超时后应保持无可路由节点错误：%v", err)
	}
	if time.Since(started) < authorizationRouteWait-200*time.Millisecond {
		t.Fatalf("授权路由等待过早结束：%s", time.Since(started))
	}
}

func TestWaitForAuthorizationDeliveredReturnsWhenAlreadyDelivered(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, _, _, err := store.IssueSignedAuthorization(context.Background(), challenge,
		testTokenLifetime(time.Minute), "req-2", func(downloadtoken.Claims) (string, error) {
			return "opaque-token", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `UPDATE download_authorizations
		SET delivered_at = '2026-01-01T00:00:03Z' WHERE id = '`+auth.Claims.AuthorizationID+`'`)

	started := time.Now()
	if err := store.waitForAuthorizationDelivered(context.Background(),
		auth.Claims.AuthorizationID, "node-1"); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) >= authorizationDeliveryPoll {
		t.Fatalf("已投递授权不应等待轮询周期：%s", time.Since(started))
	}
}

func TestWaitForAuthorizationDeliveredCancelsAndWakesNode(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	runtime := mastercontrol.NewRuntimeStore()
	runtime.StartSession(mastercontrol.Session{ID: "session-1", NodeID: "node-1"})
	store := Store{DB: db, Runtime: runtime}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, _, _, err := store.IssueSignedAuthorization(context.Background(), challenge,
		testTokenLifetime(time.Minute), "req-2", func(downloadtoken.Claims) (string, error) {
			return "opaque-token", nil
		})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- store.waitForAuthorizationDelivered(ctx,
			auth.Claims.AuthorizationID, "node-1")
	}()
	deadline := time.After(time.Second)
	for !runtime.ConsumeSyncTaskWake("node-1") {
		select {
		case <-deadline:
			t.Fatal("等待授权投递时应唤醒活跃节点会话")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("等待取消错误=%v want context.Canceled", err)
	}
}
