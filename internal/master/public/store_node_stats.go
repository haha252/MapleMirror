package public

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type nodeSLAText struct {
	H24 string
	D7  string
	D30 string
}

func (s Store) loadNodeDownloadReadyStates(ctx context.Context,
	nodeIDs []string) (map[string]nodeDownloadReadyState, error) {
	out := make(map[string]nodeDownloadReadyState, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return out, nil
	}
	query := `SELECT ni.node_id,
		COUNT(*),
		COALESCE(SUM(CASE WHEN ni.state = 'verified'
			AND ni.local_digest_sha256 = a.digest_sha256
			AND ni.size_bytes = a.size_bytes THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state = 'verified'
			AND (ni.local_digest_sha256 != a.digest_sha256 OR ni.size_bytes != a.size_bytes) THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state IS NOT NULL AND ni.state != 'verified' THEN 1 ELSE 0 END), 0)
		FROM node_inventory ni
		JOIN assets a ON a.id = ni.asset_id AND a.service_state = 'candidate'
		JOIN releases r ON r.id = a.release_id AND r.selected = 1
		JOIN projects p ON p.id = r.project_id AND p.enabled = 1
		JOIN target_inventory ti ON ti.node_id = ni.node_id
			AND ti.asset_id = ni.asset_id AND ti.desired_state = 'required'
		WHERE ni.node_id IN (` + placeholders(len(nodeIDs)) + `)
		GROUP BY ni.node_id`
	rows, err := s.DB.QueryContext(ctx, query, stringArgs(nodeIDs)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var nodeID string
		var status nodeDownloadReadyState
		if err := rows.Scan(&nodeID, &status.PublicCopies, &status.DownloadableCopies,
			&status.VerifiedMismatchCopies, &status.UnverifiedCopies); err != nil {
			return nil, err
		}
		out[nodeID] = status
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return s.loadNodePublicProbeState(ctx, nodeIDs, out)
}

func (s Store) loadNodePublicProbeState(ctx context.Context, nodeIDs []string,
	out map[string]nodeDownloadReadyState) (map[string]nodeDownloadReadyState, error) {
	if s.PublicProbeNetworkFailures <= 0 {
		return out, nil
	}
	query := `SELECT id, public_probe_network_failures,
		COALESCE(last_public_probe_error, '') FROM nodes
		WHERE id IN (` + placeholders(len(nodeIDs)) + `)`
	rows, err := s.DB.QueryContext(ctx, query, stringArgs(nodeIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		var failures int
		var probeError string
		if err := rows.Scan(&nodeID, &failures, &probeError); err != nil {
			return nil, err
		}
		status := out[nodeID]
		status.PublicProbeFailures = failures
		status.PublicProbeError = probeError
		status.PublicProbeBlocked = status.PublicProbeFailures >= s.PublicProbeNetworkFailures
		out[nodeID] = status
	}
	return out, rows.Err()
}

func (s Store) loadNodeSLATexts(ctx context.Context, nodeIDs []string) (map[string]nodeSLAText, error) {
	out := make(map[string]nodeSLAText, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		out[nodeID] = nodeSLAText{
			H24: "统计样本不足",
			D7:  "统计样本不足",
			D30: "统计样本不足",
		}
	}
	if len(nodeIDs) == 0 {
		return out, nil
	}
	start24 := timeNow().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	start7 := timeNow().Add(-24 * 7 * time.Hour).Format(time.RFC3339Nano)
	start30 := timeNow().Add(-24 * 30 * time.Hour).Format(time.RFC3339Nano)
	args := []any{start24, start24, start7, start7, start30, start30}
	args = append(args, stringArgs(nodeIDs)...)
	args = append(args, start30)
	rows, err := s.DB.QueryContext(ctx, `SELECT node_id,
		COUNT(CASE WHEN sample_start >= ? THEN 1 END),
		COALESCE(SUM(CASE WHEN sample_start >= ?
			AND routable = 1 AND heartbeat_ok = 1 THEN 1 ELSE 0 END), 0),
		COUNT(CASE WHEN sample_start >= ? THEN 1 END),
		COALESCE(SUM(CASE WHEN sample_start >= ?
			AND routable = 1 AND heartbeat_ok = 1 THEN 1 ELSE 0 END), 0),
		COUNT(CASE WHEN sample_start >= ? THEN 1 END),
		COALESCE(SUM(CASE WHEN sample_start >= ?
			AND routable = 1 AND heartbeat_ok = 1 THEN 1 ELSE 0 END), 0)
		FROM node_availability_samples
		WHERE node_id IN (`+placeholders(len(nodeIDs))+`) AND sample_start >= ?
		GROUP BY node_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		var total24, ok24, total7, ok7, total30, ok30 int64
		if err := rows.Scan(&nodeID, &total24, &ok24, &total7, &ok7,
			&total30, &ok30); err != nil {
			return nil, err
		}
		out[nodeID] = nodeSLAText{
			H24: slaRatioText(total24, ok24),
			D7:  slaRatioText(total7, ok7),
			D30: slaRatioText(total30, ok30),
		}
	}
	return out, rows.Err()
}

func (s Store) loadNodeTrafficTotals(ctx context.Context, nodeIDs []string) (map[string]int64, error) {
	out := make(map[string]int64, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return out, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT node_id, COALESCE(SUM(sent_bytes), 0)
		FROM node_traffic_totals WHERE node_id IN (`+placeholders(len(nodeIDs))+`)
		GROUP BY node_id`, stringArgs(nodeIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		var bytes int64
		if err := rows.Scan(&nodeID, &bytes); err != nil {
			return nil, err
		}
		out[nodeID] = bytes
	}
	return out, rows.Err()
}

func slaRatioText(total, ok int64) string {
	if total < 3 {
		return "统计样本不足"
	}
	return fmt.Sprintf("%.2f%%", float64(ok)*100/float64(total))
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func stringArgs(values []string) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
}
