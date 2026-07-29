package config

import "gopkg.in/yaml.v3"

func pruneUnknownYAML(current, template *yaml.Node) bool {
	if current == nil || template == nil {
		return false
	}
	if current.Kind == yaml.DocumentNode && template.Kind == yaml.DocumentNode {
		if len(current.Content) == 0 || len(template.Content) == 0 {
			return false
		}
		return pruneUnknownYAML(current.Content[0], template.Content[0])
	}
	if current.Kind != template.Kind {
		return false
	}
	switch current.Kind {
	case yaml.MappingNode:
		return pruneUnknownMap(current, template)
	case yaml.SequenceNode:
		return pruneUnknownSequence(current, template)
	default:
		return false
	}
}

func pruneUnknownMap(current, template *yaml.Node) bool {
	if len(template.Content) == 0 {
		return false
	}
	changed := false
	for i := 0; i+1 < len(current.Content); {
		key := current.Content[i]
		templateIndex := yamlMapIndex(template, key.Value)
		if templateIndex < 0 {
			current.Content = append(current.Content[:i], current.Content[i+2:]...)
			changed = true
			continue
		}
		if pruneUnknownYAML(current.Content[i+1], template.Content[templateIndex+1]) {
			changed = true
		}
		i += 2
	}
	return changed
}

func pruneUnknownSequence(current, template *yaml.Node) bool {
	if len(template.Content) != 1 || template.Content[0].Kind != yaml.MappingNode {
		return false
	}
	changed := false
	for _, item := range current.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if pruneUnknownYAML(item, template.Content[0]) {
			changed = true
		}
	}
	return changed
}
