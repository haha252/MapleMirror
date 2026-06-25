CREATE INDEX IF NOT EXISTS idx_node_availability_samples_window
ON node_availability_samples(sample_start, node_id);

CREATE INDEX IF NOT EXISTS idx_daily_node_traffic_stats_node
ON daily_node_traffic_stats(node_id, stat_day);
