package control

import (
	"context"
	"database/sql"
	"log/slog"

	"mirror-server/internal/master/assignment"
)

func (r Repository) reconcileNodeLimitChange(ctx context.Context, tx *sql.Tx, nodeID, source string, previousMax, nextMax int, now string) (int, error) {
	if previousMax == nextMax {
		return 0, nil
	}
	if r.Logger != nil {
		r.Logger.Debug(ctx, "节点镜像项目上限变化，重建目标库存",
			slog.String("node_id", nodeID),
			slog.String("source", source),
			slog.Int("previous_max_mirror_projects", previousMax),
			slog.Int("next_max_mirror_projects", nextMax))
	}
	if err := assignment.ReconcileNode(ctx, tx, nodeID, now); err != nil {
		return 0, err
	}
	return assignment.GenerateNodeTasks(ctx, tx, nodeID, now)
}
