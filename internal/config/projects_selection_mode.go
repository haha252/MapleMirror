package config

import "strings"

const (
	ProjectSelectionModeSelectors = "selectors"
	ProjectSelectionModeFile      = "file"
)

func (p Project) NormalizedDefaultSelectionMode() string {
	mode := strings.ToLower(strings.TrimSpace(p.DefaultSelectionMode))
	if mode == ProjectSelectionModeFile {
		return ProjectSelectionModeFile
	}
	return ProjectSelectionModeSelectors
}

func validProjectSelectionMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ProjectSelectionModeSelectors, ProjectSelectionModeFile:
		return true
	default:
		return false
	}
}
