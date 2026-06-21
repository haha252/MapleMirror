package public

import (
	"context"
	"database/sql"
	"testing"
	"time"
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
