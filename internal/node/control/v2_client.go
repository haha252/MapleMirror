package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/requestid"
)

func (c *Client) RunV2(ctx context.Context, wsURL string) (time.Duration, error) {
	c.v2Runtime()
	sessionClient := *c
	sessionClient.controlCtx = ctx
	return sessionClient.runV2(ctx, wsURL)
}

func (c *Client) runV2(ctx context.Context, wsURL string) (time.Duration, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = c.TLSConfig
	httpClient := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	conn, response, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: httpClient, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		if response != nil {
			return 0, fmt.Errorf("control.v2 websocket dial failed: HTTP %d: %w", response.StatusCode, err)
		}
		return 0, err
	}
	defer conn.CloseNow()
	defer transport.CloseIdleConnections()
	conn.SetReadLimit(protocolv2.MaxMessageBytes)

	helloID, _ := requestid.New()
	softwareVersion := c.SoftwareVersion
	if softwareVersion == "" {
		softwareVersion = "dev"
	}
	capabilities := c.v2Capabilities()
	hello, _ := protocolv2.New(protocolv2.TypeSessionHello, helloID, protocolv2.Hello{
		SoftwareVersion: softwareVersion,
		Capabilities:    capabilities,
	})
	if err := writeV2ClientEnvelope(ctx, conn, hello); err != nil {
		return 0, err
	}
	welcomeCtx, welcomeCancel := context.WithTimeout(ctx, 20*time.Second)
	welcomeEnvelope, err := readV2ClientEnvelope(welcomeCtx, conn)
	welcomeCancel()
	if err != nil {
		return 0, err
	}
	if welcomeEnvelope.Type == protocolv2.TypeProtocolError {
		p, _ := protocolv2.Decode[protocolv2.ProtocolError](welcomeEnvelope)
		return 0, RejectionError{Code: p.Code, Message: p.Message}
	}
	if welcomeEnvelope.Type != protocolv2.TypeSessionWelcome || welcomeEnvelope.ReplyTo != helloID {
		return 0, errors.New("control.v2 expected session.welcome")
	}
	welcome, err := protocolv2.Decode[protocolv2.Welcome](welcomeEnvelope)
	if err != nil {
		return 0, err
	}
	c.v2Runtime().beginSession()
	interval := time.Duration(welcome.StatusIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 15 * time.Second
	}
	c.logDebug("control.v2 welcome received", slog.String("node_id", c.NodeID),
		slog.String("session_id", welcome.SessionID), slog.Duration("status_interval", interval))

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.controlCtx = runCtx
	queue := controlv2.NewQueue(512, 4<<20)
	defer queue.Close()
	writerErr := make(chan error, 1)
	readerErr := make(chan error, 1)
	go func() {
		writerErr <- runV2ClientWriter(runCtx, conn, queue)
		cancel()
	}()
	go func() {
		readerErr <- c.runV2ClientReader(runCtx, conn, queue)
		cancel()
	}()

	if err := c.enqueueV2Status(queue); err != nil {
		return interval, err
	}
	if err := c.enqueueV2Durable(queue); err != nil {
		return interval, err
	}
	if err := c.enqueueV2Inventory(queue, true); err != nil {
		return interval, err
	}
	if err := c.enqueueV2SwarmState(queue); err != nil {
		return interval, err
	}
	replayTicker := time.NewTicker(time.Second)
	defer replayTicker.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pingEvery := interval
	if pingEvery > 30*time.Second || pingEvery <= 0 {
		pingEvery = 30 * time.Second
	}
	pingTicker := time.NewTicker(pingEvery)
	defer pingTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return interval, ctx.Err()
		case err := <-readerErr:
			return interval, err
		case err := <-writerErr:
			return interval, err
		case <-queue.ReplayWake():
			if err := c.enqueueV2Durable(queue); err != nil {
				return interval, err
			}
		case <-replayTicker.C:
			if err := c.flushV2Inventory(queue); err != nil && !replayBackpressure(err) {
				return interval, err
			}
			if err := queue.RetryReplay(); err != nil && !replayBackpressure(err) {
				return interval, err
			}
		case <-ticker.C:
			_ = c.enqueueV2Durable(queue)
			_ = c.enqueueV2Inventory(queue, false)
			_ = c.enqueueV2SwarmState(queue)
			if err := c.enqueueV2Status(queue); err != nil && !errors.Is(err, controlv2.ErrQueueFull) {
				return interval, err
			}
		case <-func() <-chan struct{} {
			if c.EventWake != nil {
				return c.EventWake.C()
			}
			return nil
		}():
			_ = c.enqueueV2Durable(queue)
			_ = c.enqueueV2SwarmState(queue)
			_ = c.enqueueV2Status(queue)
		case <-pingTicker.C:
			// Writes have a 10s deadline; allow a ping to wait for that write
			// lock and still receive its pong over a high-latency link.
			pingCtx, pingCancel := context.WithTimeout(runCtx, 20*time.Second)
			err := conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				return interval, err
			}
		}
	}
}

func (c *Client) v2Capabilities() []string {
	capabilities := []string{"control.v2", "status.v2", "task.attempt.v2", "swarm.v1", protocolv2.CapabilityPeerBootstrap}
	if c != nil && c.ForcePeerDownload {
		capabilities = append(capabilities, "sync.peer_only.v1")
	}
	return capabilities
}
