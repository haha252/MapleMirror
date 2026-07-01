CREATE INDEX IF NOT EXISTS idx_target_inventory_asset_state_node
ON target_inventory(asset_id, desired_state, node_id);

CREATE INDEX IF NOT EXISTS idx_node_project_assignments_project_assigned_node
ON node_project_assignments(project_id, assigned, node_id);
