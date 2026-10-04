package controlv2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestReadStreamBuffersReconnectBurstWithoutBlockingPing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan (<-chan protocolv2.Envelope), 1)
	failed := make(chan (<-chan error), 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		messages, failures := ReadStream(ctx, conn, func(ctx context.Context, c *websocket.Conn) (protocolv2.Envelope, error) {
			_, raw, err := c.Read(ctx)
			var e protocolv2.Envelope
			if err == nil {
				err = json.Unmarshal(raw, &e)
			}
			return e, err
		})
		ready <- messages
		failed <- failures
		<-ctx.Done()
	}))
	defer func() { cancel(); server.Close() }()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	go func() { _, _, _ = conn.Read(ctx) }()
	messages, failures := <-ready, <-failed
	// Reproduce the 71 pending events collected on the affected node. The
	// business consumer is deliberately paused while the burst arrives.
	for i := 0; i < 71; i++ {
		e, _ := protocolv2.New(protocolv2.TypeTrafficEvent, strconv.Itoa(i), struct{}{})
		raw, _ := json.Marshal(e)
		if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatal(err)
		}
	}
	for len(messages) != 71 {
		select {
		case err := <-failures:
			t.Fatalf("normal reconnect burst disconnected: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	pingCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err := conn.Ping(pingCtx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 71; i++ {
		if got := <-messages; got.ID != strconv.Itoa(i) {
			t.Fatalf("out of order: %s", got.ID)
		}
	}
	cancel()
}

func TestReadStreamClosesAtByteBudgetEvenBelowMessageLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	failed := make(chan (<-chan error), 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(protocolv2.MaxMessageBytes)
		_, failures := ReadStream(ctx, c, func(ctx context.Context, c *websocket.Conn) (protocolv2.Envelope, error) {
			_, raw, err := c.Read(ctx)
			var e protocolv2.Envelope
			if err == nil {
				err = json.Unmarshal(raw, &e)
			}
			return e, err
		})
		failed <- failures
		<-ctx.Done()
	}))
	defer func() { cancel(); server.Close() }()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	failures := <-failed
	e, _ := protocolv2.New(protocolv2.TypeInventorySnapshotSegment, "large", strings.Repeat("x", 900<<10))
	raw, _ := json.Marshal(e)
	for i := 0; i < 10; i++ {
		if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
			break
		}
	}
	select {
	case err := <-failures:
		var overload *InboundBacklogError
		if !errors.As(err, &overload) || overload.Messages >= InboundMaxMessages || overload.Bytes <= InboundMaxBytes {
			t.Fatalf("wrong overload reason: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
