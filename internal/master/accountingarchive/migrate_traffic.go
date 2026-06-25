package accountingarchive

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mirror-server/internal/master/accountingstate"
	"mirror-server/internal/protocol"
)

const TrafficArchiveMigrationName = "traffic_events_v8"

type TrafficMigrationOptions struct {
	Root      string
	BatchSize int
}

type TrafficMigrationResult struct {
	ArchivedRows int64
}

type trafficRow struct {
	NodeID          string
	EventSequence   uint64
	AuthorizationID string
	NodeRequestID   string
	MasterRequestID string
	SentBytes       int64
	ReportedAt      time.Time
	AccountedAt     string
	AssetID         string
	Status          string
}

func MigrateTrafficEvents(ctx context.Context, db *sql.DB, opts TrafficMigrationOptions) (TrafficMigrationResult, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1000
	}
	writer := New(opts.Root)
	if writer == nil {
		return TrafficMigrationResult{}, fmt.Errorf("归档根目录不能为空")
	}
	var result TrafficMigrationResult
	for {
		rows, err := loadTrafficRows(ctx, db, opts.BatchSize)
		if err != nil {
			return result, err
		}
		if len(rows) == 0 {
			if err := markTrafficMigrationComplete(ctx, db); err != nil {
				return result, err
			}
			return result, nil
		}
		if err := writeTrafficBatch(ctx, writer, rows); err != nil {
			return result, err
		}
		if err := applyTrafficBatch(ctx, db, rows); err != nil {
			return result, err
		}
		result.ArchivedRows += int64(len(rows))
	}
}

func EnsureTrafficArchiveReady(ctx context.Context, db *sql.DB) error {
	remaining, err := TrafficEventsRemaining(ctx, db)
	if err != nil {
		return err
	}
	if remaining == 0 {
		return nil
	}
	done, err := TrafficArchiveMigrationComplete(ctx, db)
	if err != nil {
		return err
	}
	if done {
		return fmt.Errorf("旧流量明细归档状态异常：traffic_events 仍有 %d 行", remaining)
	}
	return fmt.Errorf("旧流量明细尚未归档：traffic_events 仍有 %d 行，请先执行 -archive-accounting", remaining)
}

func loadTrafficRows(ctx context.Context, db *sql.DB, limit int) ([]trafficRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT node_id, event_sequence,
		authorization_id, node_request_id, master_request_id, sent_bytes,
		reported_at, COALESCE(accounted_at, ''), asset_id, status
		FROM traffic_events ORDER BY reported_at, node_id, event_sequence LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []trafficRow
	for rows.Next() {
		var row trafficRow
		var reportedAt string
		if err := rows.Scan(&row.NodeID, &row.EventSequence, &row.AuthorizationID,
			&row.NodeRequestID, &row.MasterRequestID, &row.SentBytes, &reportedAt,
			&row.AccountedAt, &row.AssetID, &row.Status); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339Nano, reportedAt)
		if err != nil {
			return nil, fmt.Errorf("解析旧流量事件时间失败：%w", err)
		}
		row.ReportedAt = parsed.UTC()
		out = append(out, row)
	}
	return out, rows.Err()
}

func TrafficArchiveMigrationComplete(ctx context.Context, db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM archive_migrations
		WHERE name = ?`, TrafficArchiveMigrationName).Scan(&count)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return count > 0, err
}

func TrafficEventsRemaining(ctx context.Context, db *sql.DB) (int64, error) {
	var count int64
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM traffic_events`).Scan(&count)
	return count, err
}

func writeTrafficBatch(ctx context.Context, writer *Writer, rows []trafficRow) error {
	for _, row := range rows {
		record := TrafficRecord{
			NodeID:          row.NodeID,
			EventSequence:   row.EventSequence,
			AuthorizationID: row.AuthorizationID,
			AssetID:         row.AssetID,
			NodeRequestID:   row.NodeRequestID,
			MasterRequestID: row.MasterRequestID,
			SentBytes:       row.SentBytes,
			Status:          row.Status,
			ReportedAt:      row.ReportedAt,
			AccountedAt:     row.AccountedAt,
		}
		committed := row.ReportedAt
		if row.AccountedAt != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, row.AccountedAt); err == nil {
				committed = parsed.UTC()
			}
		}
		if err := writer.WriteTraffic(ctx, record, committed); err != nil {
			return err
		}
	}
	return nil
}

func applyTrafficBatch(ctx context.Context, db *sql.DB, rows []trafficRow) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, row := range rows {
		event := protocol.TrafficEvent{
			EventSequence:   row.EventSequence,
			AuthorizationID: row.AuthorizationID,
			AssetID:         row.AssetID,
			NodeRequestID:   row.NodeRequestID,
			MasterRequestID: row.MasterRequestID,
			SentBytes:       row.SentBytes,
			Status:          row.Status,
			ReportedAt:      row.ReportedAt,
		}
		hash := accountingstate.TrafficEventHash(row.NodeID, event)
		accountedAt := row.AccountedAt
		if accountedAt == "" {
			accountedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO traffic_event_dedupe
			(node_id, event_sequence, authorization_id, event_hash, accounted_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(node_id, event_sequence) DO UPDATE SET
				authorization_id = excluded.authorization_id,
				event_hash = excluded.event_hash,
				accounted_at = excluded.accounted_at`,
			row.NodeID, row.EventSequence, row.AuthorizationID, hash, accountedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO node_traffic_cursors
			(node_id, last_event_sequence, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(node_id) DO UPDATE SET
				last_event_sequence = MAX(last_event_sequence, excluded.last_event_sequence),
				updated_at = excluded.updated_at`,
			row.NodeID, row.EventSequence, accountedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_events
			WHERE node_id = ? AND event_sequence = ?`, row.NodeID, row.EventSequence); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func markTrafficMigrationComplete(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `INSERT INTO archive_migrations
		(name, completed_at, details_json) VALUES (?, ?, '{}')
		ON CONFLICT(name) DO UPDATE SET completed_at = excluded.completed_at`,
		TrafficArchiveMigrationName, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (w *Writer) archivePath(kind string, when time.Time) string {
	return filepath.Join(w.root, kind, when.UTC().Format("2006-01-02")+".jsonl")
}

func WriteTrafficManifest(root string, result TrafficMigrationResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(root, "traffic", "migration-summary.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
