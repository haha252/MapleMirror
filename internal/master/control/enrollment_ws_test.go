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

func TestEnrollmentV2UsesWebSocketTextRPC(t *testing.T) {
	repo, closeRepo := testRepo(t)
	defer closeRepo()
	server := httptest.NewTLSServer((EnrollmentServer{Repo: repo, EnrollmentTimeout: time.Minute}).WebSocketHandler())
	defer server.Close()

	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // test server only
	client := &http.Client{Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(server.URL, "https")+"/enroll/v2",
		&websocket.DialOptions{HTTPClient: client, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	payload, _ := json.Marshal(protocol.EnrollRequest{PairingCode: "invalid", PublicName: "node-test"})
	request, _ := json.Marshal(enrollmentWSMessage{Type: protocol.TypeEnrollRequest, Payload: payload})
	if err := conn.Write(ctx, websocket.MessageText, request); err != nil {
		t.Fatal(err)
	}
	messageType, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if messageType != websocket.MessageText {
		t.Fatalf("message type=%v", messageType)
	}
	var reply enrollmentWSMessage
	if err := json.Unmarshal(data, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Type != protocol.TypeProtocolError {
		t.Fatalf("reply type=%q", reply.Type)
	}
}
