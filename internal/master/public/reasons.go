package public

import (
	"context"
	"database/sql"
	"fmt"
)

type reasonInfo struct {
	Summary string
	Detail  string
}

type replicaCounts struct {
	CandidateCount int
	CopyCount      int
	VerifiedCopies int
	NotReadyCopies int
	OfflineCopies  int
	DisabledCopies int
	MismatchCopies int
}

type nodeReadyState struct {
	ActiveSession      bool
	RequiredTargets    int
	MissingTargets     int
	OutstandingTasks   int
	FailedTasks        int
	LastReportComplete sql.NullBool
}

func (s Store) projectUnavailableInfo(ctx context.Context, projectID string) reasonInfo {
	var counts replicaCounts
	err := s.DB.QueryRowContext(ctx, `SELECT
		COUNT(DISTINCT a.id),
		COUNT(ni.asset_id),
		COALESCE(SUM(CASE WHEN ni.state = 'verified' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state = 'verified' AND n.state NOT IN ('disabled', 'offline')
			AND n.routing_ready = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state = 'verified' AND n.state = 'offline' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state = 'verified' AND n.state = 'disabled' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state IS NOT NULL AND ni.state != 'verified' THEN 1 ELSE 0 END), 0)
		FROM releases r
		JOIN assets a ON a.release_id = r.id AND a.service_state = 'candidate'
		LEFT JOIN node_inventory ni ON ni.asset_id = a.id
		LEFT JOIN nodes n ON n.id = ni.node_id
		WHERE r.project_id = ? AND r.selected = 1`, projectID).
		Scan(&counts.CandidateCount, &counts.CopyCount, &counts.VerifiedCopies, &counts.NotReadyCopies,
			&counts.OfflineCopies, &counts.DisabledCopies, &counts.MismatchCopies)
	if err != nil {
		return reasonInfo{Summary: "暂不可下载"}
	}
	return projectReasonFromCounts(counts)
}

func projectReasonFromCounts(counts replicaCounts) reasonInfo {
	detail := fmt.Sprintf("候选资产 %d，已校验副本 %d，未就绪副本 %d，离线副本 %d，禁用副本 %d，校验失败副本 %d",
		counts.CandidateCount, counts.VerifiedCopies, counts.NotReadyCopies,
		counts.OfflineCopies, counts.DisabledCopies, counts.MismatchCopies)
	switch {
	case counts.CandidateCount == 0:
		return reasonInfo{Summary: "暂无可下载资产", Detail: detail}
	case counts.VerifiedCopies == 0 && counts.MismatchCopies > 0:
		return reasonInfo{Summary: "项目副本校验未通过", Detail: detail}
	case counts.VerifiedCopies == 0:
		return reasonInfo{Summary: "项目副本尚未同步到节点", Detail: detail}
	case counts.NotReadyCopies > 0:
		return reasonInfo{Summary: "项目副本已存在，但节点尚未同步就绪", Detail: detail}
	case counts.OfflineCopies > 0 && counts.DisabledCopies == 0:
		return reasonInfo{Summary: "持有项目副本的节点当前离线", Detail: detail}
	case counts.DisabledCopies > 0 && counts.OfflineCopies == 0:
		return reasonInfo{Summary: "持有项目副本的节点已被禁用", Detail: detail}
	case counts.OfflineCopies > 0 || counts.DisabledCopies > 0:
		return reasonInfo{Summary: "持有项目副本的节点当前不可路由", Detail: detail}
	default:
		return reasonInfo{Summary: "当前没有可用下载节点", Detail: detail}
	}
}

func (s Store) assetUnavailableInfo(ctx context.Context, assetID string) reasonInfo {
	var counts replicaCounts
	err := s.DB.QueryRowContext(ctx, `SELECT
		0,
		COUNT(*),
		COALESCE(SUM(CASE WHEN ni.state = 'verified' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state = 'verified' AND n.state NOT IN ('disabled', 'offline')
			AND n.routing_ready = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state = 'verified' AND n.state = 'offline' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state = 'verified' AND n.state = 'disabled' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state IS NOT NULL AND ni.state != 'verified' THEN 1 ELSE 0 END), 0)
		FROM node_inventory ni
		JOIN nodes n ON n.id = ni.node_id
		WHERE ni.asset_id = ?`, assetID).
		Scan(&counts.CandidateCount, &counts.CopyCount, &counts.VerifiedCopies, &counts.NotReadyCopies,
			&counts.OfflineCopies, &counts.DisabledCopies, &counts.MismatchCopies)
	if err != nil {
		return reasonInfo{Summary: "暂不可下载"}
	}
	detail := fmt.Sprintf("副本总数 %d，已校验副本 %d，未就绪副本 %d，离线副本 %d，禁用副本 %d，校验失败副本 %d",
		counts.CopyCount, counts.VerifiedCopies, counts.NotReadyCopies,
		counts.OfflineCopies, counts.DisabledCopies, counts.MismatchCopies)
	switch {
	case counts.VerifiedCopies == 0 && counts.MismatchCopies > 0:
		return reasonInfo{Summary: "副本校验未通过", Detail: detail}
	case counts.VerifiedCopies == 0 && counts.CopyCount > 0:
		return reasonInfo{Summary: "副本尚未完成校验", Detail: detail}
	case counts.VerifiedCopies == 0:
		return reasonInfo{Summary: "暂无节点副本", Detail: detail}
	case counts.NotReadyCopies > 0:
		return reasonInfo{Summary: "持有副本的节点尚未同步就绪", Detail: detail}
	case counts.OfflineCopies > 0 && counts.DisabledCopies == 0:
		return reasonInfo{Summary: "持有副本的节点当前离线", Detail: detail}
	case counts.DisabledCopies > 0 && counts.OfflineCopies == 0:
		return reasonInfo{Summary: "持有副本的节点已被禁用", Detail: detail}
	case counts.OfflineCopies > 0 || counts.DisabledCopies > 0:
		return reasonInfo{Summary: "持有副本的节点当前不可路由", Detail: detail}
	default:
		return reasonInfo{Summary: "当前没有可用下载节点", Detail: detail}
	}
}

func (s Store) nodeRoutingReadyInfo(ctx context.Context, nodeID, state, lastHeartbeat string) reasonInfo {
	switch state {
	case "disabled":
		return reasonInfo{Summary: "节点已被管理员禁用"}
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
		WHERE node_id = ? AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')`, nodeID).Scan(&status.OutstandingTasks); err != nil {
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

func (s Store) hasActiveSession(ctx context.Context, nodeID string) (bool, error) {
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
