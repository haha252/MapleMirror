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
}

func (c Client) sendFullInventoryReport(conn net.Conn, reqID string, sequence uint64,
	taskBudget *int) (uint64, error) {
	if c.DB == nil {
		return sequence, nil
	}
	cursor, err := c.loadInventoryCursor()
	if err != nil {
		return sequence, err
	}
	if !shouldSendFullInventoryReport(cursor, time.Now().UTC()) {
		return sequence, nil
	}
	if err := c.refreshLocalInventory(); err != nil {
		return sequence, err
	}
	items, err := c.loadInventoryItems()
	if err != nil {
		return sequence, err
	}
	revision := cursor.NextRevision
	if revision == 0 {
		revision = 1
	}
	chunks := inventoryChunks(items)
	generatedAt := time.Now().UTC()
	for i, chunk := range chunks {
		reportID, _ := requestid.New()
		report := protocol.InventoryReport{
			ReportID:    reportID,
			Revision:    revision,
			GeneratedAt: generatedAt,
			Complete:    i == len(chunks)-1,
			Items:       chunk,
		}
		if err := c.sendInventoryReport(conn, reqID, sequence, report); err != nil {
			return sequence, err
		}
		sequence++
		var next uint64
		next, err = c.readOptionalTasksWithBudget(conn, reqID, sequence, taskBudget)
		if err != nil {
			return sequence, err
		}
		sequence = next
	}
	if err := c.storeInventoryCursor(inventoryCursor{
		NextRevision:      revision + 1,
		LastAckedRevision: revision,
	}); err != nil {
		return sequence, err
	}
	return sequence, nil
}

func (c Client) sendInventoryReport(conn net.Conn, reqID string, sequence uint64, report protocol.InventoryReport) error {
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
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       report.ReportID,
		MessageType:     protocol.TypeInventoryReport,
		SentAt:          time.Now().UTC(),
		NodeID:          c.NodeID,
		RequestID:       reqID,
		Sequence:        sequence,
		Payload:         body,
	}); err != nil {
		return err
	}
	msg, err := c.readExpectedResponse(conn, reqID, protocol.TypeHeartbeatAck)
	if err != nil {
		return err
	}
	if msg.MessageType != protocol.TypeHeartbeatAck {
		return fmt.Errorf("库存上报收到非预期响应类型: %s", msg.MessageType)
	}
	return nil
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
	err := c.DB.QueryRow(`SELECT next_revision, last_acked_revision, updated_at
		FROM inventory_report_cursor WHERE id = 1`).
		Scan(&cursor.NextRevision, &cursor.LastAckedRevision, &cursor.UpdatedAt)
	return cursor, err
}

func (c Client) storeInventoryCursor(cursor inventoryCursor) error {
	_, err := c.DB.Exec(`UPDATE inventory_report_cursor
		SET next_revision = ?, last_acked_revision = ?, updated_at = ?
		WHERE id = 1`,
		cursor.NextRevision, cursor.LastAckedRevision,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func shouldSendFullInventoryReport(cursor inventoryCursor, now time.Time) bool {
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
