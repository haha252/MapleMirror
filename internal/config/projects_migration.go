package config

import "gopkg.in/yaml.v3"

func migrateProjectsArchitectureDefault(doc *yaml.Node) (bool, bool) {
	root := yamlRoot(doc)
	projects := findYAMLMapValue(root, "projects")
	if projects == nil || projects.Kind != yaml.SequenceNode {
		return false, false
	}
	changed := false
	found := false
	for _, item := range projects.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		oldIndex := yamlMapIndex(item, "architecture_default_enabled")
		if oldIndex < 0 {
			continue
		}
		found = true
		if yamlMapIndex(item, "architecture_match_enabled") < 0 {
			item.Content[oldIndex].Value = "architecture_match_enabled"
			item.Content[oldIndex].HeadComment = "由旧架构开关自动迁移；后续请直接维护 architecture_match_enabled。"
			item.Content[oldIndex].LineComment = ""
			item.Content[oldIndex+1].HeadComment = ""
			item.Content[oldIndex+1].LineComment = ""
			changed = true
			continue
		}
		item.Content = append(item.Content[:oldIndex], item.Content[oldIndex+2:]...)
		changed = true
	}
	return changed, found
}
