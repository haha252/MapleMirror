package public

import (
	"context"
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

func (s Store) projectUnavailableInfo(ctx context.Context, projectID string) reasonInfo {
	var counts replicaCounts
	err := s.DB.QueryRowContext(ctx, `SELECT
		COUNT(DISTINCT a.id),
		COUNT(ni.asset_id),
		COALESCE(SUM(CASE WHEN ni.state = 'verified' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN ni.state = 'verified' AND n.state NOT IN ('disabled', 'offline')
			AND (n.last_heartbeat_at IS NULL OR n.last_heartbeat_at = ''
				OR n.public_download_base_url = '')
			THEN 1 ELSE 0 END), 0),
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
	detail := fmt.Sprintf("候选资产 %d，已校验副本 %d，未就绪副本 %d，离线副本 %d",
		counts.CandidateCount, counts.VerifiedCopies, counts.NotReadyCopies, counts.OfflineCopies)
	switch {
	case counts.CandidateCount == 0:
		return reasonInfo{Summary: "暂无可下载资产", Detail: detail}
	case counts.VerifiedCopies == 0 && counts.MismatchCopies > 0:
		return reasonInfo{Summary: "项目副本暂不可下载", Detail: detail}
	case counts.VerifiedCopies == 0:
		return reasonInfo{Summary: "项目副本尚未同步到节点", Detail: detail}
	case counts.NotReadyCopies > 0:
		return reasonInfo{Summary: "项目副本已存在，但节点尚未下载就绪", Detail: detail}
	case counts.OfflineCopies > 0 && counts.DisabledCopies == 0:
		return reasonInfo{Summary: "持有项目副本的节点当前离线", Detail: detail}
	case counts.DisabledCopies > 0 && counts.OfflineCopies == 0:
		return reasonInfo{Summary: "当前没有可用下载节点", Detail: detail}
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
			AND (n.last_heartbeat_at IS NULL OR n.last_heartbeat_at = ''
				OR n.public_download_base_url = '')
			THEN 1 ELSE 0 END), 0),
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
	detail := fmt.Sprintf("副本总数 %d，已校验副本 %d，未就绪副本 %d，离线副本 %d",
		counts.CopyCount, counts.VerifiedCopies, counts.NotReadyCopies, counts.OfflineCopies)
	switch {
	case counts.VerifiedCopies == 0 && counts.MismatchCopies > 0:
		return reasonInfo{Summary: "副本暂不可下载", Detail: detail}
	case counts.VerifiedCopies == 0 && counts.CopyCount > 0:
		return reasonInfo{Summary: "副本尚未完成校验", Detail: detail}
	case counts.VerifiedCopies == 0:
		return reasonInfo{Summary: "暂无节点副本", Detail: detail}
	case counts.NotReadyCopies > 0:
		return reasonInfo{Summary: "持有副本的节点尚未下载就绪", Detail: detail}
	case counts.OfflineCopies > 0 && counts.DisabledCopies == 0:
		return reasonInfo{Summary: "持有副本的节点当前离线", Detail: detail}
	case counts.DisabledCopies > 0 && counts.OfflineCopies == 0:
		return reasonInfo{Summary: "当前没有可用下载节点", Detail: detail}
	case counts.OfflineCopies > 0 || counts.DisabledCopies > 0:
		return reasonInfo{Summary: "持有副本的节点当前不可路由", Detail: detail}
	default:
		return reasonInfo{Summary: "当前没有可用下载节点", Detail: detail}
	}
}
