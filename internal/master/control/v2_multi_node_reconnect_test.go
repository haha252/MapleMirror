package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mirror-server/internal/controltls"
	nodecontrol "mirror-server/internal/node/control"
	"mirror-server/internal/storage"
)

func TestV2FiveNodesRecoverInventoryAndTrafficTogether(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	seedTrafficAuth(t, repo)
	peers := make(map[string]*x509.Certificate)
	databases := make(map[string]*sql.DB)
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("node-%d", i)
		authID := fmt.Sprintf("auth-%d", i)
		cert := testV2PeerCertificate(t)
		peers[id] = cert
		if i == 1 {
			mustExecControl(t, repo.DB, `UPDATE node_certificates SET fingerprint=? WHERE id='cert-1'`, controltls.Fingerprint(cert))
		} else {
			mustExecControl(t, repo.DB, `INSERT INTO nodes
    (id,public_name,state,target_bandwidth_bps,last_heartbeat_at,routing_ready,created_at,updated_at)
    SELECT ?,? ,state,target_bandwidth_bps,last_heartbeat_at,routing_ready,created_at,updated_at FROM nodes WHERE id='node-1'`, id, id)
			mustExecControl(t, repo.DB, `INSERT INTO node_certificates
    (id,node_id,serial_number,fingerprint,not_before,not_after,status,issued_request_id,created_at)
    SELECT ?,?,?,?,not_before,not_after,status,issued_request_id,created_at FROM node_certificates WHERE id='cert-1'`, fmt.Sprintf("cert-%d", i), id, fmt.Sprint(i), controltls.Fingerprint(cert))
			mustExecControl(t, repo.DB, `INSERT INTO download_authorizations
    (id,asset_id,node_id,client_prefix_key,issued_at,expires_at,max_bytes,range_limit,status,request_id)
    SELECT ?,asset_id,?,client_prefix_key,issued_at,expires_at,max_bytes,range_limit,status,request_id FROM download_authorizations WHERE id='auth-1'`, authID, id)
		}
		db, err := storage.OpenNode(filepath.Join(t.TempDir(), "node.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		databases[id] = db
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		for j := 1; j <= 200; j++ {
			_, err := tx.Exec(`INSERT INTO pending_traffic_events
    (event_sequence,authorization_id,node_request_id,master_request_id,sent_bytes,created_at,asset_id,status)
    VALUES(?,?,?,'master-req-1',1,?,'asset-1','completed')`, j, authID, fmt.Sprintf("%s-req-%d", id, j), time.Now().UTC().Format(time.RFC3339Nano))
			if err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	master := &V2Server{Repo: repo, StatusInterval: time.Second}
	handler := master.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{peers[r.URL.Query().Get("node")]}}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	errors := make(chan error, 5)
	var workers sync.WaitGroup
	for id, db := range databases {
		workers.Add(1)
		go func(id string, db *sql.DB) {
			defer workers.Done()
			client := &nodecontrol.Client{NodeID: id, DB: db, V2Runtime: nodecontrol.NewV2Runtime()}
			_, err := client.RunV2(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/control/v2?node="+id)
			if ctx.Err() == nil {
				errors <- fmt.Errorf("%s disconnected: %w", id, err)
			}
		}(id, db)
	}
	defer func() { cancel(); workers.Wait() }()
	for {
		ready := 0
		for id, db := range databases {
			var pending, revision int
			if err := db.QueryRow(`SELECT COUNT(*) FROM pending_traffic_events WHERE confirmed_at IS NULL`).Scan(&pending); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT last_acked_revision FROM inventory_report_cursor WHERE id=1`).Scan(&revision); err != nil && err != sql.ErrNoRows {
				t.Fatal(err)
			}
			latest, ok := repo.runtime().LatestV2Status(id)
			if pending == 0 && revision > 0 && ok && time.Since(latest.ReportedAt) < 2*time.Second {
				ready++
			}
		}
		if ready == 5 {
			break
		}
		select {
		case err := <-errors:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatalf("only %d/5 nodes recovered", ready)
		case <-time.After(20 * time.Millisecond):
		}
	}
	var total int64
	if err := repo.DB.QueryRow(`SELECT SUM(sent_bytes) FROM daily_asset_stats WHERE asset_id='asset-1'`).Scan(&total); err != nil || total != 1000 {
		t.Fatalf("five-node accounting=%d want=1000 err=%v", total, err)
	}
}
