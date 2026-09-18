package mirrorsync

import (
	"context"
	"fmt"

	"mirror-server/internal/config"
)

func (s Service) TriggerVersionReset(ctx context.Context, projectID, requestID string) (string, error) {
	if projectID == "" {
		return "", fmt.Errorf("版本重置必须指定项目")
	}
	return s.trigger(ctx, projectID, requestID, true)
}

func (s Service) triggerAll(ctx context.Context, projects config.Projects, requestID string) (string, error) {
	var firstScanID string
	for _, project := range projects.Projects {
		if !project.Enabled {
			continue
		}
		summary, err := s.scanProject(ctx, projects, project.ID, requestID, false)
		if firstScanID == "" {
			firstScanID = summary.ScanID
		}
		if err != nil {
			return firstScanID, err
		}
	}
	return firstScanID, nil
}

func (s Service) scanProject(ctx context.Context, projects config.Projects, projectID, requestID string, resetVersionBaseline bool) (ScanSummary, error) {
	var summary ScanSummary
	var err error
	if resetVersionBaseline {
		summary, err = s.Scanner.ScanVersionReset(ctx, projects, projectID, requestID)
	} else {
		summary, err = s.Scanner.Scan(ctx, projects, projectID, requestID)
	}
	s.scheduleNextScan(ctx, summary.ProjectID, err)
	return summary, err
}
