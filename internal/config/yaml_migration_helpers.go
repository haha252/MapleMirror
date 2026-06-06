package config

import "gopkg.in/yaml.v3"

func yamlRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
		return doc.Content[0]
	}
	return doc
}

func yamlMapIndex(node *yaml.Node, key string) int {
	if node == nil || node.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func yamlRemoveMapKey(node *yaml.Node, key string) bool {
	index := yamlMapIndex(node, key)
	if index < 0 {
		return false
	}
	node.Content = append(node.Content[:index], node.Content[index+2:]...)
	return true
}

func yamlEnsureMap(parent *yaml.Node, key, comment string) *yaml.Node {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil
	}
	if existing := findYAMLMapValue(parent, key); existing != nil {
		if existing.Kind == yaml.MappingNode {
			return existing
		}
		return nil
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, yamlStringKey(key, comment), child)
	return child
}

func yamlEnsureSequence(parent *yaml.Node, key, comment string) *yaml.Node {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil
	}
	if existing := findYAMLMapValue(parent, key); existing != nil {
		if existing.Kind == yaml.SequenceNode {
			return existing
		}
		return nil
	}
	child := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	parent.Content = append(parent.Content, yamlStringKey(key, comment), child)
	return child
}

func yamlStringKey(value, comment string) *yaml.Node {
	return &yaml.Node{
		Kind: yaml.ScalarNode, Tag: "!!str", Value: value,
		HeadComment: comment,
	}
}

func yamlSequenceHasScalar(seq *yaml.Node, value string) bool {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return false
	}
	for _, item := range seq.Content {
		if item.Kind == yaml.ScalarNode && item.Value == value {
			return true
		}
	}
	return false
}
