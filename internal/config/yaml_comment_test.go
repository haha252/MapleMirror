package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func assertEveryYAMLKeyHasChineseComment(t *testing.T, data []byte) {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var missing []string
	walkYAMLKeys(&doc, "", &missing)
	if len(missing) > 0 {
		t.Fatalf("配置模板存在缺少中文注释的字段：%s", strings.Join(missing, ", "))
	}
}

func walkYAMLKeys(node *yaml.Node, path string, missing *[]string) {
	if node == nil {
		return
	}
	if node.Kind == yaml.DocumentNode {
		for _, child := range node.Content {
			walkYAMLKeys(child, path, missing)
		}
		return
	}
	if node.Kind == yaml.SequenceNode {
		for _, child := range node.Content {
			walkYAMLKeys(child, path+"[]", missing)
		}
		return
	}
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]
		nextPath := joinYAMLPath(path, key.Value)
		if !hasChineseComment(key) {
			*missing = append(*missing, nextPath)
		}
		walkYAMLKeys(value, nextPath, missing)
	}
}

func hasChineseComment(node *yaml.Node) bool {
	comment := node.HeadComment + node.LineComment + node.FootComment
	for _, r := range comment {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

func joinYAMLPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
