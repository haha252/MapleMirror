package control

func eligibleSyncTaskSQL(taskTable string) string {
	return ` AND (
	(` + taskTable + `.task_type = 'asset_download' AND EXISTS (
		SELECT 1 FROM target_inventory ti
		JOIN assets ca ON ca.id = ti.asset_id
		JOIN releases cr ON cr.id = ca.release_id
		JOIN projects cp ON cp.id = cr.project_id
		WHERE ti.node_id = ` + taskTable + `.node_id AND ti.asset_id = ` + taskTable + `.asset_id
		AND ti.desired_state = 'required'
		AND ca.service_state IN ('candidate', 'pending', 'active')
		AND cr.selected = 1 AND cp.enabled = 1
	))
	OR (` + taskTable + `.task_type = 'asset_delete' AND EXISTS (
		SELECT 1 FROM target_inventory ti
		JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.node_id = ` + taskTable + `.node_id AND ti.asset_id = ` + taskTable + `.asset_id
		AND ti.desired_state = 'remove' AND ni.state = 'verified'
	))
	OR ` + taskTable + `.task_type NOT IN ('asset_download', 'asset_delete'))`
}
