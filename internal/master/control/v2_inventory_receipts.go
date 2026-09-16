package control

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
)

// acceptV2InventorySegmentReceipt makes each segment idempotent inside one
// inventory revision. The receipt is committed in the same transaction as the
// inventory mutations, so a stored receipt always means the segment's effects
// were committed too.
func acceptV2InventorySegmentReceipt(ctx context.Context, tx *sql.Tx, nodeID string,
	segment protocolv2.InventorySnapshotSegment, messageID, now string) (bool, error) {
	body, err := json.Marshal(segment)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(sum[:])
	complete := 0
	if segment.Complete {
		complete = 1
	}

	var existingID, existingHash string
	var existingComplete, existingCount int
	err = tx.QueryRowContext(ctx, `SELECT message_id,payload_hash,complete,item_count
		FROM node_inventory_v2_segments
		WHERE node_id=? AND revision=? AND segment=?`,
		nodeID, segment.Revision, segment.Segment).
		Scan(&existingID, &existingHash, &existingComplete, &existingCount)
	if err == nil {
		if existingID != messageID || existingHash != payloadHash || existingComplete != complete || existingCount != len(segment.Items) {
			return false, fmt.Errorf("inventory segment identity reused with different payload")
		}
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_inventory_v2_segments
		(node_id,revision,segment,message_id,payload_hash,complete,item_count,received_at)
		VALUES(?,?,?,?,?,?,?,?)`, nodeID, segment.Revision, segment.Segment,
		messageID, payloadHash, complete, len(segment.Items), now)
	return false, err
}

func pruneV2InventorySegmentReceipts(ctx context.Context, tx *sql.Tx, nodeID string, keepRevision uint64) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM node_inventory_v2_segments
		WHERE node_id=? AND revision < ?`, nodeID, keepRevision)
	return err
}

func inventoryReceiptTime() string { return time.Now().UTC().Format(time.RFC3339Nano) }
