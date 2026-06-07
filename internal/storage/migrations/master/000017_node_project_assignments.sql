ALTER TABLE nodes ADD COLUMN max_mirror_projects INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN project_assignment_mode TEXT NOT NULL DEFAULT 'auto';

CREATE TABLE IF NOT EXISTS node_project_assignments (
    node_id TEXT NOT NULL REFERENCES nodes(id),
    project_id TEXT NOT NULL REFERENCES projects(id),
    mode TEXT NOT NULL,
    assigned INTEGER NOT NULL,
    score INTEGER NOT NULL DEFAULT 0,
    pinned INTEGER NOT NULL DEFAULT 0,
    last_changed_at TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (node_id, project_id)
);

CREATE INDEX IF NOT EXISTS idx_node_project_assignments_node
    ON node_project_assignments(node_id, assigned, score);
