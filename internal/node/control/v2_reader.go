package control

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/coder/websocket"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/requestid"
)

func (c *Client) enqueueV2Status(queue *controlv2.Queue) error {
	runtime := c.v2Runtime()
	runtime.capacityMu.Lock()
	defer runtime.capacityMu.Unlock()
	return c.enqueueV2StatusLocked(queue)
}

// Caller holds capacityMu through sampling and admission to the coalesced queue.
func (c *Client) enqueueV2StatusLocked(queue *controlv2.Queue) error {
	runtime := c.v2Runtime()
	runtime.capacityRevision++
	id, _ := requestid.New()
	status := protocolv2.NodeStatus{
		CapacityRevision: runtime.capacityRevision,
		MirrorTraffic:    c.Activity.SampleTraffic(),
		Status:           "syncing", PublicDownloadBaseURL: c.PublicDownloadBaseURL,
		MaxMirrorProjects: c.MaxMirrorProjects, SyncTaskSlotsAvailable: c.availableSyncTaskSlots(),
		TargetBandwidthBPS: c.TargetBandwidthBPS, ActualBandwidthBPS: c.sampleBandwidth(),
		ActiveTasks: c.loadV2ActiveTasks(),
	}
	status.DownloadPressure = c.sampleDownloadPressure(status.ActualBandwidthBPS)
	if c.Activity != nil {
		status.PublicActiveDownloads = c.Activity.PublicDownloads()
		status.SwarmActiveUploads = c.Activity.SwarmUploads()
	}
	if c.Capacity != nil {
		snapshot := c.Capacity.Snapshot()
		status.AssetFS = protocolv2.FilesystemCapacity{AvailableBytes: snapshot.Asset.AvailableBytes, TotalBytes: snapshot.Asset.TotalBytes, ReservedBytes: snapshot.ReservedAsset, Valid: snapshot.Asset.Valid}
		status.PartialFS = protocolv2.FilesystemCapacity{AvailableBytes: snapshot.Partial.AvailableBytes, TotalBytes: snapshot.Partial.TotalBytes, ReservedBytes: snapshot.ReservedPartial, Valid: snapshot.Partial.Valid}
	}
	envelope, _ := protocolv2.New(protocolv2.TypeNodeStatus, id, status)
	return queue.Enqueue(envelope, c.NodeID)
}

func (c *Client) runV2ClientReader(ctx context.Context, conn *websocket.Conn, queue *controlv2.Queue) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Canceled)
	readerClient := *c
	readerClient.controlCtx = ctx
	c = &readerClient
	messages, failures := controlv2.ReadStream(ctx, conn, readV2ClientEnvelope)
	go func() {
		select {
		case err := <-failures:
			cancel(err)
		case <-ctx.Done():
		}
	}()
	for {
		var envelope protocolv2.Envelope
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case envelope = <-messages:
		}
		if envelope.Type == protocolv2.TypeProtocolError {
			p, _ := protocolv2.Decode[protocolv2.ProtocolError](envelope)
			return fmt.Errorf("control.v2 protocol error %s: %s", p.Code, p.Message)
		}
		started := time.Now()
		if err := c.handleV2Inbound(ctx, queue, envelope); err != nil {
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			return err
		}
		if time.Since(started) >= time.Second && c.Logger != nil {
			c.Logger.Warn(ctx, "control.v2 消息处理缓慢", slog.String("type", envelope.Type), slog.Duration("duration", time.Since(started)))
		}
	}
}

func (c *Client) handleV2Inbound(_ context.Context, queue *controlv2.Queue, envelope protocolv2.Envelope) (err error) {
	identity, isACK, err := replayACKIdentity(envelope)
	if err != nil {
		return err
	}
	if isACK {
		if queue.CompletedACK(envelope.ReplyTo, identity) {
			return nil
		}
		defer func() {
			if err == nil {
				queue.CompleteACK(envelope.ReplyTo, identity)
			}
		}()
		if handled, ackErr := c.handleFrozenStatusACK(queue, envelope); handled {
			return ackErr
		}
	}
	switch envelope.Type {
	case protocolv2.TypeSyncTask:
		return c.handleV2SyncTask(queue, envelope)
	case protocolv2.TypeSyncCancel:
		return c.handleV2Cancel(envelope)
	case protocolv2.TypeSyncResultAck:
		return c.handleV2ResultAck(envelope)
	case protocolv2.TypeInventorySnapshotAck:
		return c.handleV2InventoryAck(envelope)
	default:
		return c.handleV2BusinessInbound(queue, envelope)
	}
}
