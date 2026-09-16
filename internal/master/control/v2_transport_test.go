package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"mirror-server/internal/controltls"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestReadV2EnvelopeRejectsBinaryMalformedAndOversize(t *testing.T) {
	tests := []struct {
		name        string
		messageType websocket.MessageType
		data        []byte
	}{
		{name: "binary", messageType: websocket.MessageBinary, data: []byte(`{}`)},
		{name: "malformed json", messageType: websocket.MessageText, data: []byte(`{"v":`)},
		{name: "oversize", messageType: websocket.MessageText, data: []byte(strings.Repeat("x", protocolv2.MaxMessageBytes+1))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errCh := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					errCh <- err
					return
				}
				defer conn.CloseNow()
				conn.SetReadLimit(protocolv2.MaxMessageBytes)
				ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
				defer cancel()
				_, err = readV2Envelope(ctx, conn)
				errCh <- err
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			if err := conn.Write(ctx, tc.messageType, tc.data); err != nil && tc.name != "oversize" {
				t.Fatal(err)
			}
			select {
			case err := <-errCh:
				if err == nil {
					t.Fatal("invalid websocket message was accepted")
				}
			case <-ctx.Done():
				t.Fatal("server did not reject invalid websocket message")
			}
		})
	}
}

func TestControlV2NewSessionTakesOverSameNode(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	cert := testV2PeerCertificate(t)
	fingerprint := controltls.Fingerprint(cert)
	now := time.Now().UTC()
	mustExecControl(t, repo.DB, `INSERT INTO nodes
		(id,public_name,certificate_fingerprint,state,target_bandwidth_bps,routing_ready,created_at,updated_at)
		VALUES('node-v2','node-v2',?,'syncing',0,0,?,?)`, fingerprint,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	mustExecControl(t, repo.DB, `INSERT INTO node_certificates
		(id,node_id,serial_number,fingerprint,not_before,not_after,status,issued_request_id,created_at)
		VALUES('cert-v2','node-v2','1',?,?,?,'active','req',?)`, fingerprint,
		now.Add(-time.Minute).Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	v2 := &V2Server{Repo: repo, StatusInterval: time.Second, HeartbeatTimeout: 5 * time.Second}
	base := v2.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
		base.ServeHTTP(w, r)
	}))
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/control/v2"

	first := dialV2TestSession(t, url, "hello-first")
	defer first.CloseNow()
	second := dialV2TestSession(t, url, "hello-second")
	defer second.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := first.Read(ctx)
	if err == nil {
		t.Fatal("older control.v2 session stayed readable after takeover")
	}

	var count int
	if err := repo.DB.QueryRow(`SELECT COUNT(*) FROM node_control_sessions WHERE node_id='node-v2' AND disconnected_at IS NULL`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("active durable sessions=%d want=1", count)
	}
	if !repo.runtime().ActiveSession("node-v2") {
		t.Fatal("runtime lost replacement session")
	}
}

func dialV2TestSession(t *testing.T, url, helloID string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		t.Fatal(err)
	}
	hello, _ := protocolv2.New(protocolv2.TypeSessionHello, helloID, protocolv2.Hello{SoftwareVersion: "test"})
	if err := writeV2Envelope(ctx, conn, hello); err != nil {
		conn.CloseNow()
		t.Fatal(err)
	}
	welcome, err := readV2Envelope(ctx, conn)
	if err != nil {
		conn.CloseNow()
		t.Fatal(err)
	}
	if welcome.Type != protocolv2.TypeSessionWelcome || welcome.ReplyTo != helloID {
		conn.CloseNow()
		t.Fatalf("welcome=%+v", welcome)
	}
	return conn
}

func testV2PeerCertificate(t *testing.T) *x509.Certificate {
	t.Helper()
	_, certPEM := publicProbeTestCertificate(t)
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("test certificate PEM decode failed")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
