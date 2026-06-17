package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/requestid"
)

const inventoryChunkSize = 1000
const inventoryReportMinInterval = time.Minute

type inventoryCursor struct {
	NextRevision      uint64
	LastAckedRevision uint64
	UpdatedAt         string
	ForceRequestedAt  string
}

type pendingInventoryReport struct {
	Revision         uint64
	GeneratedAt      time.Time
	Chunks           [][]protocol.InventoryItem
	Index            int
	ForceRequestedAt string
}

func (c Client) sendFullInventoryReport(conn net.Conn, reqID string, sequence uint64) (uint64, error) {
	var pending *pendingInventoryReport
	for {
		next, sent, err := c.sendNextInventoryReportChunk(conn, reqID, sequence,
			&pending)
		if err != nil || !sent {
			return next, err
		}
		sequence = next
	}
}

func (c Client) sendNextInventoryReportChunk(conn net.Conn, reqID string,
	sequence uint64, pending **pendingInventoryReport) (uint64, bool, error) {
	if c.DB == nil {
		return sequence, false, nil
	}
	if *pending == nil {
		report, err := c.prepareInventoryReport()
		if err != nil || report == nil {
			return sequence, false, err
		}
		*pending = report
	}
	report := *pending
	chunk := report.Chunks[report.Index]
	reportID, _ := requestid.New()
	complete := report.Index == len(report.Chunks)-1
	var slots *int
	if complete {
		available := c.availableSyncTaskSlots()
		slots = &available
	}
	next, err := c.sendInventoryReport(conn, reqID, sequence, protocol.InventoryReport{
		ReportID:               reportID,
		Revision:               report.Revision,
		GeneratedAt:            report.GeneratedAt,
		Complete:               complete,
		Items:                  chunk,
		SyncTaskSlotsAvailable: slots,
	})
	if err != nil {
		return sequence, false, err
	}
	report.Index++
	if report.Index >= len(report.Chunks) {
		err = c.storeInventoryCursor(inventoryCursor{
			NextRevision:      report.Revision + 1,
			LastAckedRevision: report.Revision,
			ForceRequestedAt:  report.ForceRequestedAt,
		})
		if err != nil {
			return sequence, false, err
		}
		*pending = nil
	}
	next, err = c.readOptionalTasksToCapacity(conn, reqID, next)
	if err != nil {
		return sequence, false, err
	}
	return next, true, err
}

func (c Client) prepareInventoryReport() (*pendingInventoryReport, error) {
	cursor, err := c.loadInventoryCursor()
	if err != nil {
		return nil, err
	}
	if !shouldSendFullInventoryReport(cursor, time.Now().UTC()) {
		return nil, nil
	}
	items, err := c.loadInventoryItems()
	if err != nil {
		return nil, err
	}
	revision := cursor.NextRevision
	if revision == 0 {
		revision = 1
	}
	return &pendingInventoryReport{
		Revision: revision, GeneratedAt: time.Now().UTC(),
		Chunks:           inventoryChunks(items),
		ForceRequestedAt: cursor.ForceRequestedAt,
	}, nil
}

func (c Client) sendInventoryReport(conn net.Conn, reqID string, sequence uint64,
	report protocol.InventoryReport) (uint64, error) {
	body, _ := json.Marshal(report)
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点发送完整库存上报",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.Uint64("sequence", sequence),
			slog.Uint64("revision", report.Revision),
			slog.Int("item_count", len(report.Items)),
			slog.Bool("complete", report.Complete))
	}
	messageID := report.ReportID
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       messageID,
		MessageType:     protocol.TypeInventoryReport,
		SentAt:          time.Now().UTC(),
		NodeID:          c.NodeID,
		RequestID:       reqID,
		Sequence:        sequence,
		Payload:         body,
	}); err != nil {
		return sequence, err
	}
	next := sequence + 1
	msg, err := c.readExpectedAck(conn, reqID, &next, protocol.TypeHeartbeatAck, messageID)
	if err != nil {
		return sequence, err
	}
	if msg.MessageType != protocol.TypeHeartbeatAck {
		return sequence, fmt.Errorf("库存上报收到非预期响应类型: %s", msg.MessageType)
	}
	return next, nil
}

func (c Client) loadInventoryItems() ([]protocol.InventoryItem, error) {
	rows, err := c.DB.Query(`SELECT asset_id, size_bytes, digest_sha256, state
		FROM local_assets ORDER BY asset_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []protocol.InventoryItem
	for rows.Next() {
		var item protocol.InventoryItem
		if err := rows.Scan(&item.AssetID, &item.SizeBytes, &item.DigestSHA256, &item.LocalState); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c Client) loadInventoryCursor() (inventoryCursor, error) {
	if _, err := c.DB.Exec(`INSERT OR IGNORE INTO inventory_report_cursor
		(id, next_revision, last_acked_revision, updated_at)
		VALUES (1, 1, 0, ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return inventoryCursor{}, err
	}
	var cursor inventoryCursor
	err := c.DB.QueryRow(`SELECT next_revision, last_acked_revision, updated_at,
		COALESCE(force_report_requested_at, '')
		FROM inventory_report_cursor WHERE id = 1`).
		Scan(&cursor.NextRevision, &cursor.LastAckedRevision, &cursor.UpdatedAt,
			&cursor.ForceRequestedAt)
	return cursor, err
}

func (c Client) storeInventoryCursor(cursor inventoryCursor) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := c.DB.Exec(`UPDATE inventory_report_cursor
		SET next_revision = ?, last_acked_revision = ?, updated_at = ?,
			force_report_requested_at = CASE
				WHEN COALESCE(force_report_requested_at, '') = ? THEN NULL
				ELSE force_report_requested_at
			END
		WHERE id = 1`,
		cursor.NextRevision, cursor.LastAckedRevision, now, cursor.ForceRequestedAt)
	return err
}

func shouldSendFullInventoryReport(cursor inventoryCursor, now time.Time) bool {
	if cursor.ForceRequestedAt != "" {
		return true
	}
	if cursor.LastAckedRevision == 0 || cursor.UpdatedAt == "" {
		return true
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, cursor.UpdatedAt)
	if err != nil {
		return true
	}
	return now.Sub(updatedAt) >= inventoryReportMinInterval
}

func inventoryChunks(items []protocol.InventoryItem) [][]protocol.InventoryItem {
	if len(items) == 0 {
		return [][]protocol.InventoryItem{{}}
	}
	chunks := make([][]protocol.InventoryItem, 0, (len(items)+inventoryChunkSize-1)/inventoryChunkSize)
	for start := 0; start < len(items); start += inventoryChunkSize {
		end := start + inventoryChunkSize
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[start:end])
	}
	return chunks
}
