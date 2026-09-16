package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"mirror-server/internal/protocol"
)

func (e Enroller) exchangeWS(messageType string, payload any) (protocol.Envelope, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return protocol.Envelope{}, err
	}
	data, err := json.Marshal(struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}{Type: messageType, Payload: body})
	if err != nil {
		return protocol.Envelope{}, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = e.TLSConfig
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(ctx, e.WSAddress, &websocket.DialOptions{
		HTTPClient: client, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		if response != nil {
			return protocol.Envelope{}, fmt.Errorf("enrollment.v2 WSS dial failed: HTTP %d: %w", response.StatusCode, err)
		}
		return protocol.Envelope{}, err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(protocol.MaxFrameBytes)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		return protocol.Envelope{}, err
	}
	gotType, replyData, err := conn.Read(ctx)
	if err != nil {
		return protocol.Envelope{}, err
	}
	if gotType != websocket.MessageText {
		return protocol.Envelope{}, errors.New("enrollment.v2 requires text JSON responses")
	}
	var reply struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(replyData, &reply); err != nil {
		return protocol.Envelope{}, err
	}
	_ = conn.Close(websocket.StatusNormalClosure, "enrollment RPC complete")
	return protocol.Envelope{ProtocolVersion: protocol.Version, MessageType: reply.Type, Payload: reply.Payload}, nil
}
