package control

func eligibleSyncTaskSQL(taskTable string) string {
	return ` AND (` + taskTable + `.task_type != 'asset_download' OR EXISTS (
		SELECT 1 FROM target_inventory ti
		JOIN assets ca ON ca.id = ti.asset_id
		JOIN releases cr ON cr.id = ca.release_id
		JOIN projects cp ON cp.id = cr.project_id
		WHERE ti.node_id = ` + taskTable + `.node_id AND ti.asset_id = ` + taskTable + `.asset_id
		AND ti.desired_state = 'required'
		AND ca.service_state IN ('candidate', 'pending')
		AND cr.selected = 1 AND cp.enabled = 1
	))`
}
