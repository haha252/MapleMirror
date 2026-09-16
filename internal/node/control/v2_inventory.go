package control

import (
	"errors"
	"strconv"
	"time"

	"mirror-server/internal/controlv2"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func (c *Client) enqueueV2Inventory(queue *controlv2.Queue, force bool) error {
	if c.DB == nil {
		return nil
	}
	runtime := c.v2Runtime()
	runtime.mu.Lock()
	if runtime.inventoryPending != 0 {
		runtime.mu.Unlock()
		return nil
	}
	runtime.mu.Unlock()
	cursor, err := c.loadInventoryCursor()
	if err != nil {
		return err
	}
	if !force && !shouldSendFullInventoryReport(cursor, time.Now().UTC()) {
		return nil
	}
	items, err := c.loadInventoryItems()
	if err != nil {
		return err
	}
	revision := cursor.NextRevision
	if revision == 0 {
		revision = 1
	}
	// Reserve the revision before enqueueing any segment. A reconnect or process
	// restart starts a fresh revision instead of rebuilding different inventory
	// contents under an already-used revision identity.
	if err := c.reserveV2InventoryRevision(revision); err != nil {
		return err
	}
	chunks := inventoryChunks(items)
	if len(chunks) == 0 {
		chunks = [][]protocol.InventoryItem{{}}
	}
	generated := time.Now().UTC()
	for i, chunk := range chunks {
		converted := make([]protocolv2.InventoryItem, 0, len(chunk))
		for _, it := range chunk {
			converted = append(converted, protocolv2.InventoryItem{AssetID: it.AssetID, SizeBytes: it.SizeBytes, DigestSHA256: it.DigestSHA256, LocalState: it.LocalState})
		}
		body := protocolv2.InventorySnapshotSegment{Revision: revision, Segment: i, Complete: i == len(chunks)-1, GeneratedAt: generated, Items: converted}
		id := protocolv2.StableMessageID(protocolv2.TypeInventorySnapshotSegment, c.NodeID, strconv.FormatUint(revision, 10), strconv.Itoa(i))
		env, _ := protocolv2.New(protocolv2.TypeInventorySnapshotSegment, id, body)
		if err := queue.Enqueue(env, ""); err != nil {
			return err
		}
	}
	runtime.mu.Lock()
	runtime.inventoryPending = revision
	runtime.inventoryPendingID = protocolv2.StableMessageID(protocolv2.TypeInventorySnapshotSegment, c.NodeID, strconv.FormatUint(revision, 10), strconv.Itoa(len(chunks)-1))
	runtime.inventorySentAt = time.Now()
	runtime.mu.Unlock()
	return nil
}

func (c *Client) handleV2InventoryAck(envelope protocolv2.Envelope) error {
	ack, err := protocolv2.Decode[protocolv2.InventorySnapshotAck](envelope)
	if err != nil {
		return err
	}
	runtime := c.v2Runtime()
	runtime.mu.Lock()
	pending := runtime.inventoryPending
	pendingID := runtime.inventoryPendingID
	runtime.mu.Unlock()
	if pending == 0 || ack.Revision != pending {
		return nil
	}
	if pendingID == "" || envelope.ReplyTo != pendingID {
		return errors.New("inventory snapshot ack reply_to mismatch")
	}
	cursor, err := c.loadInventoryCursor()
	if err != nil {
		return err
	}
	cursor.LastAckedRevision = ack.Revision
	if cursor.NextRevision <= ack.Revision {
		cursor.NextRevision = ack.Revision + 1
	}
	if err := c.storeInventoryCursor(cursor); err != nil {
		return err
	}
	runtime.mu.Lock()
	runtime.inventoryPending = 0
	runtime.inventoryPendingID = ""
	runtime.mu.Unlock()
	return nil
}

func (c *Client) reserveV2InventoryRevision(revision uint64) error {
	if c.DB == nil {
		return nil
	}
	_, err := c.DB.Exec(`UPDATE inventory_report_cursor
		SET next_revision = CASE WHEN next_revision <= ? THEN ? ELSE next_revision END
		WHERE id = 1`, revision, revision+1)
	return err
}
