package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/controltls"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2LegacyBatchSurvivesBlockedDatabaseAndKeepsPingMoving(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	seedTrafficAuth(t, repo)
	cert := testV2PeerCertificate(t)
	mustExecControl(t, repo.DB, `UPDATE node_certificates SET fingerprint=?`, controltls.Fingerprint(cert))
	master := &V2Server{Repo: repo, StatusInterval: time.Second}
	base := master.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
		base.ServeHTTP(w, r)
	}))
	defer server.Close()
	conn := dialV2TestSession(t, "ws"+strings.TrimPrefix(server.URL, "http")+"/control/v2", "legacy-burst")
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	acks := make(chan error, 1)
	go func() {
		for i := 0; i < 50; i++ {
			e, err := readV2Envelope(ctx, conn)
			if err != nil {
				acks <- err
				return
			}
			if e.Type != protocolv2.TypeTrafficEventAck {
				acks <- fmt.Errorf("unexpected reply %s", e.Type)
				return
			}
		}
		acks <- nil
		_, _ = readV2Envelope(ctx, conn)
	}()
	// Hold the only SQLite connection while an unchanged old node sends its
	// entire batch. The transport reader must continue independently.
	tx, err := repo.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 1; i <= 50; i++ {
		e, _ := protocolv2.New(protocolv2.TypeTrafficEvent, protocolv2.StableMessageID(protocolv2.TypeTrafficEvent, strconv.Itoa(i)), protocolv2.TrafficEvent{
			EventSequence: uint64(i), AuthorizationID: "auth-1", AssetID: "asset-1", SentBytes: 1,
			NodeRequestID: "node-req", MasterRequestID: "master-req-1", Status: "completed", ReportedAt: time.Now().UTC(),
		})
		if err := writeV2Envelope(ctx, conn, e); err != nil {
			t.Fatal(err)
		}
	}
	pingCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err := conn.Ping(pingCtx); err != nil {
		t.Fatalf("business backlog blocked transport: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-acks:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var sent int64
	if err := repo.DB.QueryRow(`SELECT sent_bytes FROM daily_asset_stats WHERE asset_id='asset-1'`).Scan(&sent); err != nil || sent != 50 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	// A subsequent protocol-level Ping proves the session remains usable.
	if err := conn.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}
