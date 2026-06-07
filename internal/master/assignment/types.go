package assignment

import (
	"errors"
	"time"
)

const (
	ModeAuto   = "auto"
	ModeManual = "manual"

	heatWindowDays       = 30
	minRetention         = 72 * time.Hour
	replacementThreshold = 1.2
)

var ErrManualLimitExceeded = errors.New("手动分配项目数超过节点上限")

type Project struct {
	ID            string `json:"project_id"`
	Name          string `json:"name"`
	Score         int64  `json:"score"`
	Assigned      bool   `json:"assigned"`
	Pinned        bool   `json:"pinned"`
	LastChangedAt string `json:"last_changed_at,omitempty"`
}

type NodeProjects struct {
	NodeID            string    `json:"node_id"`
	AssignmentMode    string    `json:"assignment_mode"`
	MaxMirrorProjects int       `json:"max_mirror_projects"`
	Projects          []Project `json:"projects"`
}

type projectScore struct {
	id            string
	name          string
	score         int64
	assigned      bool
	pinned        bool
	lastChangedAt string
}
