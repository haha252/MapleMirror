package control

import (
	"time"

	"mirror-server/internal/node/localasset"
)

type localInventoryRecord struct {
	AssetID      string
	RelativePath string
	DigestSHA256 string
	SizeBytes    int64
	State        string
}

func (c Client) RefreshLocalInventory() error {
	return c.refreshLocalInventory()
}

func (c Client) refreshLocalInventory() error {
	if c.DB == nil || c.Storage == "" {
		return nil
	}
	records, err := c.loadLocalInventoryRecords()
	if err != nil {
		return err
	}
	for _, record := range records {
		state := c.verifyLocalRecord(record)
		if state != record.State {
			if err := c.updateLocalInventoryRecord(record.AssetID, state); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c Client) loadLocalInventoryRecords() ([]localInventoryRecord, error) {
	rows, err := c.DB.Query(`SELECT asset_id, relative_path, digest_sha256,
		size_bytes, state FROM local_assets ORDER BY asset_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []localInventoryRecord
	for rows.Next() {
		var item localInventoryRecord
		if err := rows.Scan(&item.AssetID, &item.RelativePath, &item.DigestSHA256,
			&item.SizeBytes, &item.State); err != nil {
			return nil, err
		}
		records = append(records, item)
	}
	return records, rows.Err()
}

func (c Client) verifyLocalRecord(record localInventoryRecord) string {
	return localasset.Verify(c.Storage, localasset.Record{
		RelativePath: record.RelativePath,
		DigestSHA256: record.DigestSHA256,
		SizeBytes:    record.SizeBytes,
	})
}

func (c Client) updateLocalInventoryRecord(assetID, state string) error {
	_, err := c.DB.Exec(`UPDATE local_assets SET state = ?,
		verified_at = ? WHERE asset_id = ?`,
		state, time.Now().UTC().Format(time.RFC3339Nano), assetID)
	return err
}
