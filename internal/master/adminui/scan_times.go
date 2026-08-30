package adminui

import (
	"context"

	"mirror-server/internal/master/mirrorsync"
)

func (s *Server) scanStatesResponse(ctx context.Context, items []mirrorsync.ProjectScanState) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	names := s.projectNames(ctx)
	for _, item := range items {
		out = append(out, map[string]any{
			"project_id": item.ProjectID, "project_name": names[item.ProjectID], "enabled": item.Enabled,
			"last_scan_started_at":   s.displayTime(item.LastScanStartedAt),
			"last_scan_completed_at": s.displayTime(item.LastScanCompletedAt),
			"last_scan_id":           item.LastScanID,
			"last_scan_state":        item.LastScanState,
			"next_scan_at":           s.displayTime(item.NextScanAt),
			"last_error_message":     item.LastErrorMessage,
			"updated_at":             s.displayTime(item.UpdatedAt),
		})
	}
	return out
}

func (s *Server) scanSummaryResponse(item mirrorsync.ScanSummary) map[string]any {
	return map[string]any{
		"scan_id": item.ScanID, "project_id": item.ProjectID,
		"state": item.State, "selected_releases": item.SelectedReleases,
		"accepted_assets": item.AcceptedAssets, "rejected_assets": item.RejectedAssets,
		"request_id": item.RequestID, "started_at": s.displayTime(item.StartedAt),
		"completed_at":           s.displayTime(item.CompletedAt),
		"next_scan_at":           s.displayTime(item.NextScanAt),
		"last_scan_started_at":   s.displayTime(item.LastScanStartedAt),
		"last_scan_completed_at": s.displayTime(item.LastScanCompletedAt),
		"last_error_message":     item.LastErrorMessage,
	}
}
