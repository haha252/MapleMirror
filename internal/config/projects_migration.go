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

func migrateSingleProjectArchitectureDefault(doc *yaml.Node) (bool, bool) {
	root := yamlRoot(doc)
	return migrateProjectArchitectureDefault(root)
}

func migrateProjectsLegacyRegex(doc *yaml.Node) bool {
	root := yamlRoot(doc)
	projects := findYAMLMapValue(root, "projects")
	if projects == nil || projects.Kind != yaml.SequenceNode {
		return false
	}
	changed := false
	for _, item := range projects.Content {
		if migrateProjectLegacyRegex(item) {
			changed = true
		}
	}
	return changed
}

func migrateSingleProjectLegacyRegex(doc *yaml.Node) bool {
	return migrateProjectLegacyRegex(yamlRoot(doc))
}

func migrateProjectArchitectureDefault(item *yaml.Node) (bool, bool) {
	if item == nil || item.Kind != yaml.MappingNode {
		return false, false
	}
	oldIndex := yamlMapIndex(item, "architecture_default_enabled")
	if oldIndex < 0 {
		return false, false
	}
	if yamlMapIndex(item, "architecture_match_enabled") < 0 {
		item.Content[oldIndex].Value = "architecture_match_enabled"
		item.Content[oldIndex].HeadComment = "由旧架构开关自动迁移；后续请直接维护 architecture_match_enabled。"
		item.Content[oldIndex].LineComment = ""
		item.Content[oldIndex+1].HeadComment = ""
		item.Content[oldIndex+1].LineComment = ""
		return true, true
	}
	item.Content = append(item.Content[:oldIndex], item.Content[oldIndex+2:]...)
	return true, true
}

func migrateProjectLegacyRegex(item *yaml.Node) bool {
	if item == nil || item.Kind != yaml.MappingNode {
		return false
	}
	changed := false
	movedArchitectureEnabled := false
	movedSystemEnabled := false
	for _, move := range []struct {
		from string
		to   string
	}{
		{"architecture_match_enabled", "architecture_match_enabled"},
		{"architecture_regex", "architecture_regex"},
		{"system_match_enabled", "system_match_enabled"},
		{"system_regex", "system_regex"},
	} {
		index := yamlMapIndex(item, move.from)
		if index < 0 {
			continue
		}
		regex := projectRegexMap(item)
		if regex == nil {
			continue
		}
		targetIndex := yamlMapIndex(regex, move.to)
		if targetIndex < 0 {
			key := cloneYAMLNode(item.Content[index])
			key.Value = move.to
			regex.Content = append(regex.Content, key, cloneYAMLNode(item.Content[index+1]))
		} else if significantYAMLScalar(item.Content[index+1]) {
			regex.Content[targetIndex+1] = cloneYAMLNode(item.Content[index+1])
		}
		if move.from == "architecture_match_enabled" {
			movedArchitectureEnabled = true
		}
		if move.from == "system_match_enabled" {
			movedSystemEnabled = true
		}
		item.Content = append(item.Content[:index], item.Content[index+2:]...)
		changed = true
	}
	if movedArchitectureEnabled && ensureRegexScalar(item, "architecture_regex") {
		changed = true
	}
	if movedSystemEnabled && ensureRegexScalar(item, "system_regex") {
		changed = true
	}
	return changed
}

func significantYAMLScalar(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.ScalarNode {
		return false
	}
	return node.Value != "" && node.Value != "false"
}

func projectRegexMap(item *yaml.Node) *yaml.Node {
	pipeline := yamlEnsureMap(item, "asset_pipeline", "")
	if pipeline == nil {
		return nil
	}
	classify := yamlEnsureMap(pipeline, "classify", "")
	if classify == nil {
		return nil
	}
	regex := yamlEnsureMap(classify, "regex", "")
	return regex
}

func ensureRegexScalar(item *yaml.Node, key string) bool {
	regex := projectRegexMap(item)
	if regex == nil || yamlMapIndex(regex, key) >= 0 {
		return false
	}
	regex.Content = append(regex.Content, yamlStringKey(key, ""),
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""})
	return true
}
