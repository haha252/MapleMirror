package public

import (
	"context"
	"database/sql"
	"fmt"
)

type nodeReadyState struct {
	ActiveSession      bool
	RequiredTargets    int
	MissingTargets     int
	OutstandingTasks   int
	FailedTasks        int
	LastReportComplete sql.NullBool
}

type nodeDownloadReadyState struct {
	PublicCopies           int
	DownloadableCopies     int
	VerifiedMismatchCopies int
	UnverifiedCopies       int
	PublicProbeBlocked     bool
	PublicProbeFailures    int
	PublicProbeError       string
}

func (s Store) nodeRoutingReadyInfo(ctx context.Context, nodeID, state, lastHeartbeat string) reasonInfo {
	switch state {
	case "disabled":
		return reasonInfo{Summary: "节点当前不可用"}
	case "offline":
		if reason := s.latestCloseReason(ctx, nodeID); reason != "" {
			return reasonInfo{Summary: reason}
		}
		return reasonInfo{Summary: "节点离线或心跳超时"}
	}

	status, err := s.loadNodeReadyState(ctx, nodeID)
	if err != nil {
		return reasonInfo{Summary: "未满足同步就绪条件"}
	}
	if !status.ActiveSession && lastHeartbeat == "" {
		return reasonInfo{Summary: "控制连接尚未建立"}
	}

	detail := fmt.Sprintf("目标库存 %d 项，缺失校验 %d 项，未完成任务 %d 个，失败任务 %d 个，活动会话 %s，最近库存上报 %s",
		status.RequiredTargets, status.MissingTargets, status.OutstandingTasks, status.FailedTasks,
		yesNo(status.ActiveSession), inventoryReportLabel(status.LastReportComplete))
	switch {
	case status.FailedTasks > 0:
		return reasonInfo{Summary: "存在失败的同步任务，等待重试或人工重置", Detail: detail}
	case status.OutstandingTasks > 0:
		return reasonInfo{Summary: "仍有同步任务未完成", Detail: detail}
	case status.RequiredTargets == 0:
		return reasonInfo{Summary: "主节点尚未下发目标库存", Detail: detail}
	case status.MissingTargets > 0 && !status.LastReportComplete.Valid:
		return reasonInfo{Summary: "尚未上报完整库存，未完成最终对账", Detail: detail}
	case status.MissingTargets > 0 && !status.LastReportComplete.Bool:
		return reasonInfo{Summary: "库存上报尚未完成，未完成最终对账", Detail: detail}
	case status.MissingTargets > 0:
		return reasonInfo{Summary: "目标库存尚未全部校验完成", Detail: detail}
	case !status.ActiveSession:
		return reasonInfo{Summary: "最近心跳正常，但当前控制会话记录未保持活动", Detail: detail}
	default:
		return reasonInfo{Summary: "未满足同步就绪条件", Detail: detail}
	}
}

func (s Store) nodeDownloadReadyInfo(ctx context.Context, nodeID, state, lastHeartbeat, downloadURL string, status nodeDownloadReadyState) reasonInfo {
	switch state {
	case "disabled":
		return reasonInfo{Summary: "节点当前不可用"}
	case "offline":
		if reason := s.latestCloseReason(ctx, nodeID); reason != "" {
			return reasonInfo{Summary: reason}
		}
		return reasonInfo{Summary: "节点离线或心跳超时"}
	}

	detail := fmt.Sprintf("公开副本 %d 项，可下载副本 %d 项，未完成校验副本 %d 项，最近心跳 %s，公网下载地址 %s",
		status.PublicCopies, status.DownloadableCopies, status.UnverifiedCopies,
		blankAsDash(lastHeartbeat), yesNo(downloadURL != ""))
	switch {
	case status.PublicCopies == 0:
		return reasonInfo{Summary: "暂无可公开下载的资产副本", Detail: detail}
	case downloadURL == "":
		return reasonInfo{Summary: "节点尚未提供公网下载地址", Detail: detail}
	case lastHeartbeat == "":
		return reasonInfo{Summary: "节点尚未上报心跳，不能提供下载", Detail: detail}
	case status.PublicProbeBlocked:
		probeDetail := fmt.Sprintf("%s，公网探测连续失败 %d 次，最近错误 %s",
			detail, status.PublicProbeFailures, blankAsDash(status.PublicProbeError))
		return reasonInfo{Summary: "节点在线，但公网下载入口探测失败", Detail: probeDetail}
	case status.VerifiedMismatchCopies > 0:
		return reasonInfo{Summary: "副本暂不可下载", Detail: detail}
	case status.DownloadableCopies > 0:
		return reasonInfo{Summary: "下载就绪", Detail: detail}
	default:
		return reasonInfo{Summary: "副本尚未完成校验", Detail: detail}
	}
}

func (s Store) loadNodeReadyState(ctx context.Context, nodeID string) (nodeReadyState, error) {
	activeSession, err := s.hasActiveSession(ctx, nodeID)
	if err != nil {
		return nodeReadyState{}, err
	}
	var status nodeReadyState
	status.ActiveSession = activeSession
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM target_inventory
		WHERE node_id = ? AND desired_state = 'required'`, nodeID).Scan(&status.RequiredTargets); err != nil {
		return nodeReadyState{}, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.node_id = ? AND ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified')`, nodeID).Scan(&status.MissingTargets); err != nil {
		return nodeReadyState{}, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM node_tasks
		WHERE node_id = ? AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')`,
		nodeID).Scan(&status.OutstandingTasks); err != nil {
		return nodeReadyState{}, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM node_tasks
		WHERE node_id = ? AND state = 'failed'`, nodeID).Scan(&status.FailedTasks); err != nil {
		return nodeReadyState{}, err
	}
	status.LastReportComplete, err = s.latestInventoryReportComplete(ctx, nodeID)
	if err != nil {
		return nodeReadyState{}, err
	}
	return status, nil
}

func (s Store) loadNodeDownloadReadyState(ctx context.Context, nodeID string) (nodeDownloadReadyState, error) {
	var status nodeDownloadReadyState
	if err := s.DB.QueryRowContext(ctx, `SELECT
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
		WHERE ni.node_id = ?`, nodeID).
		Scan(&status.PublicCopies, &status.DownloadableCopies,
			&status.VerifiedMismatchCopies, &status.UnverifiedCopies); err != nil {
		return nodeDownloadReadyState{}, err
	}
	if s.PublicProbeNetworkFailures > 0 {
		err := s.DB.QueryRowContext(ctx, `SELECT public_probe_network_failures,
			COALESCE(last_public_probe_error, '') FROM nodes WHERE id = ?`, nodeID).
			Scan(&status.PublicProbeFailures, &status.PublicProbeError)
		if err != nil {
			return nodeDownloadReadyState{}, err
		}
		status.PublicProbeBlocked = status.PublicProbeFailures >= s.PublicProbeNetworkFailures
	}
	return status, nil
}

func (s Store) hasActiveSession(ctx context.Context, nodeID string) (bool, error) {
	if s.Runtime != nil {
		return s.Runtime.ActiveSession(nodeID), nil
	}
	var activeSession int
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM node_control_sessions
		WHERE node_id = ? AND disconnected_at IS NULL
	)`, nodeID).Scan(&activeSession)
	if err != nil {
		return false, err
	}
	return activeSession == 1, nil
}

func (s Store) latestCloseReason(ctx context.Context, nodeID string) string {
	var reason string
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(close_reason, '')
		FROM node_control_sessions
		WHERE node_id = ? AND close_reason IS NOT NULL AND close_reason != ''
		ORDER BY connected_at DESC LIMIT 1`, nodeID).Scan(&reason)
	if err != nil {
		return ""
	}
	return reason
}

func (s Store) latestInventoryReportComplete(ctx context.Context, nodeID string) (sql.NullBool, error) {
	if s.Runtime != nil {
		_, complete, ok := s.Runtime.LatestInventoryState(nodeID)
		return sql.NullBool{Bool: complete, Valid: ok}, nil
	}
	var complete sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT complete
		FROM node_inventory_reports
		WHERE node_id = ?
		ORDER BY reported_at DESC LIMIT 1`, nodeID).Scan(&complete)
	if err == sql.ErrNoRows {
		return sql.NullBool{}, nil
	}
	if err != nil {
		return sql.NullBool{}, err
	}
	return sql.NullBool{Bool: complete.Int64 == 1, Valid: complete.Valid}, nil
}
