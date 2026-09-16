package control

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"mirror-server/internal/protocol"
)

func TestEnrollerExchangeWS(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		messageType, data, err := conn.Read(ctx)
		if err != nil || messageType != websocket.MessageText {
			return
		}
		var request struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &request) != nil || request.Type != protocol.TypeEnrollRequest {
			return
		}
		body, _ := json.Marshal(protocol.EnrollPending{EnrollmentID: "enroll-1"})
		reply, _ := json.Marshal(struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}{Type: protocol.TypeEnrollPending, Payload: body})
		_ = conn.Write(ctx, websocket.MessageText, reply)
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	serverTLS := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	serverTLS.MinVersion = tls.VersionTLS12

	enroller := Enroller{WSAddress: "wss" + strings.TrimPrefix(server.URL, "https"), TLSConfig: serverTLS}
	reply, err := enroller.exchangeWS(protocol.TypeEnrollRequest, protocol.EnrollRequest{PairingCode: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if reply.MessageType != protocol.TypeEnrollPending {
		t.Fatalf("reply type=%q", reply.MessageType)
	}
}
