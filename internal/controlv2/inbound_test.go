package controlv2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestReadStreamRespondsToPingWhileBusinessConsumerIsBlocked(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ReadStream(ctx, conn, func(ctx context.Context, conn *websocket.Conn) (protocolv2.Envelope, error) {
			_, raw, err := conn.Read(ctx)
			var envelope protocolv2.Envelope
			if err == nil {
				err = json.Unmarshal(raw, &envelope)
			}
			return envelope, err
		})
		close(ready)
		<-ctx.Done() // Deliberately never consume the business message.
	}))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	go func() { _, _, _ = conn.Read(ctx) }()
	<-ready
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"node.status"}`)); err != nil {
		t.Fatal(err)
	}
	pingCtx, pingCancel := context.WithTimeout(ctx, time.Second)
	defer pingCancel()
	if err := conn.Ping(pingCtx); err != nil {
		t.Fatalf("business backlog blocked pong: %v", err)
	}
	cancel()
}
