package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mirror-server/internal/controltls"
	nodecontrol "mirror-server/internal/node/control"
	"mirror-server/internal/storage"
)

func TestV2EndToEndDrains1000EventsAfterConnectionLossWithoutDoubleAccounting(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	seedTrafficAuth(t, repo)
	cert := testV2PeerCertificate(t)
	mustExecControl(t, repo.DB, `UPDATE node_certificates SET fingerprint=? WHERE id='cert-1'`, controltls.Fingerprint(cert))
	nodeDB, err := storage.OpenNode(filepath.Join(t.TempDir(), "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer nodeDB.Close()
	tx, err := nodeDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 1000; i++ {
		_, err := tx.Exec(`INSERT INTO pending_traffic_events
		 (event_sequence,authorization_id,node_request_id,master_request_id,sent_bytes,created_at,asset_id,status)
		 VALUES(?,'auth-1',?,'master-req-1',1,?,'asset-1','completed')`, i, fmt.Sprintf("req-%d", i), time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	master := &V2Server{Repo: repo, StatusInterval: time.Second}
	handler := master.Handler()
	var connections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
		connections.Add(1)
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := &nodecontrol.Client{DB: nodeDB, NodeID: "node-1", PublicDownloadBaseURL: "https://node.example.test", V2Runtime: nodecontrol.NewV2Runtime()}
	done := make(chan error, 1)
	go func() {
		for {
			_, err := client.RunV2(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/control/v2")
			if ctx.Err() != nil {
				done <- err
				return
			}
			if connections.Load() > 2 {
				done <- fmt.Errorf("unexpected reconnect: %w", err)
				return
			}
		}
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("client did not stop")
		}
	}()
	dropped := false
	for {
		var pending, acked int
		if err := nodeDB.QueryRow(`SELECT COUNT(*) FROM pending_traffic_events WHERE confirmed_at IS NULL`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if !dropped && pending <= 950 {
			if conn, ok := master.registry.Load("node-1"); ok {
				_ = conn.(interface{ CloseNow() error }).CloseNow()
				dropped = true
			}
		}
		_ = nodeDB.QueryRow(`SELECT last_acked_revision FROM inventory_report_cursor WHERE id=1`).Scan(&acked)
		if dropped && pending == 0 && acked >= 2 {
			break
		}
		select {
		case err := <-done:
			done <- err
			t.Fatalf("control session failed: %v", err)
		case <-ctx.Done():
			t.Fatalf("replay did not drain: pending=%d inventory=%d", pending, acked)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if got := connections.Load(); got != 2 {
		t.Fatalf("connections=%d, expected only the injected reconnect", got)
	}
	var sent int64
	if err := repo.DB.QueryRow(`SELECT sent_bytes FROM daily_asset_stats WHERE asset_id='asset-1'`).Scan(&sent); err != nil || sent != 1000 {
		t.Fatalf("accounted=%d want=1000 err=%v", sent, err)
	}
	latest, ok := repo.runtime().LatestV2Status("node-1")
	if !ok || time.Since(latest.ReportedAt) > 2*time.Second {
		t.Fatalf("status starved while draining: %+v", latest)
	}
}
