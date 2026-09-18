package mirrorsync

import (
	"context"
	"fmt"

	"mirror-server/internal/config"
)

type scanOptions struct {
	resetVersionBaseline bool
}

func (s Scanner) ScanVersionReset(ctx context.Context, projects config.Projects, projectID, requestID string) (ScanSummary, error) {
	if projectID == "" {
		return ScanSummary{}, fmt.Errorf("版本重置必须指定项目")
	}
	return s.scanWithOptions(ctx, projects, projectID, requestID, scanOptions{resetVersionBaseline: true})
}
