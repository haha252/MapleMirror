package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type yamlMigration func(*yaml.Node) bool

func repairYAML(data, example []byte, migrations ...yamlMigration) ([]byte, bool, error) {
	if len(example) == 0 {
		return data, false, nil
	}
	var current yaml.Node
	if err := yaml.Unmarshal(data, &current); err != nil {
		return nil, false, fmt.Errorf("解析 YAML 配置失败：%w", err)
	}
	var template yaml.Node
	if err := yaml.Unmarshal(example, &template); err != nil {
		return nil, false, fmt.Errorf("解析内置配置模板失败：%w", err)
	}
	changed := false
	for _, migration := range migrations {
		if migration(&current) {
			changed = true
		}
	}
	if mergeMissingYAML(&current, &template) {
		changed = true
	}
	if !changed {
		return data, false, nil
	}
	encoded, err := yaml.Marshal(&current)
	if err != nil {
		return nil, false, fmt.Errorf("编码 YAML 配置失败：%w", err)
	}
	return encoded, true, nil
}

func mergeMissingYAML(current, template *yaml.Node) bool {
	if current == nil || template == nil {
		return false
	}
	if current.Kind == 0 {
		*current = *cloneYAMLNode(template)
		return true
	}
	changed := mergeYAMLComments(current, template)
	if current.Kind == yaml.DocumentNode && template.Kind == yaml.DocumentNode {
		if len(template.Content) == 0 {
			return changed
		}
		if len(current.Content) == 0 {
			current.Content = []*yaml.Node{cloneYAMLNode(template.Content[0])}
			return true
		}
		return mergeMissingYAML(current.Content[0], template.Content[0]) || changed
	}
	if current.Kind != template.Kind {
		return changed
	}
	switch template.Kind {
	case yaml.MappingNode:
		return mergeMissingMap(current, template) || changed
	case yaml.SequenceNode:
		return mergeMissingSequence(current, template) || changed
	default:
		return changed
	}
}

func mergeMissingMap(current, template *yaml.Node) bool {
	changed := false
	for i := 0; i+1 < len(template.Content); i += 2 {
		key := template.Content[i]
		value := template.Content[i+1]
		existingIndex := yamlMapIndex(current, key.Value)
		if existingIndex < 0 {
			current.Content = append(current.Content, cloneYAMLNode(key), missingYAMLValue(value))
			changed = true
			continue
		}
		if mergeYAMLComments(current.Content[existingIndex], key) {
			changed = true
		}
		existing := current.Content[existingIndex+1]
		if mergeMissingYAML(existing, value) {
			changed = true
		}
	}
	return changed
}

func mergeMissingSequence(current, template *yaml.Node) bool {
	if len(template.Content) != 1 || template.Content[0].Kind != yaml.MappingNode {
		return false
	}
	changed := false
	for _, item := range current.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if mergeYAMLComments(item, template.Content[0]) {
			changed = true
		}
		if mergeMissingYAML(item, template.Content[0]) {
			changed = true
		}
	}
	return changed
}

func mergeYAMLComments(current, template *yaml.Node) bool {
	changed := false
	if current.HeadComment == "" && template.HeadComment != "" {
		current.HeadComment = template.HeadComment
		changed = true
	}
	if current.LineComment == "" && template.LineComment != "" {
		current.LineComment = template.LineComment
		changed = true
	}
	if current.FootComment == "" && template.FootComment != "" {
		current.FootComment = template.FootComment
		changed = true
	}
	return changed
}

func findYAMLMapValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func missingYAMLValue(template *yaml.Node) *yaml.Node {
	clone := cloneYAMLNode(template)
	clearYAMLSequenceItems(clone)
	return clone
}

func clearYAMLSequenceItems(node *yaml.Node) {
	if node == nil {
		return
	}
	if node.Kind == yaml.SequenceNode {
		node.Content = nil
		return
	}
	for _, child := range node.Content {
		clearYAMLSequenceItems(child)
	}
}

func yamlHasPath(data []byte, path ...string) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	return nodeHasPath(&doc, path)
}

func nodeHasPath(node *yaml.Node, path []string) bool {
	if len(path) == 0 {
		return true
	}
	if node == nil {
		return false
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return false
		}
		return nodeHasPath(node.Content[0], path)
	}
	if path[0] == "[]" && node.Kind == yaml.SequenceNode {
		for _, child := range node.Content {
			if nodeHasPath(child, path[1:]) {
				return true
			}
		}
		return false
	}
	if node.Kind != yaml.MappingNode {
		return false
	}
	return nodeHasPath(findYAMLMapValue(node, path[0]), path[1:])
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	clone := *node
	if len(node.Content) > 0 {
		clone.Content = make([]*yaml.Node, 0, len(node.Content))
		for _, child := range node.Content {
			clone.Content = append(clone.Content, cloneYAMLNode(child))
		}
	}
	return &clone
}
