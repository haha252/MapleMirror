package config

import "gopkg.in/yaml.v3"

func migrateQuotaBlacklist(doc *yaml.Node) (bool, bool) {
	root := yamlRoot(doc)
	oldList := findYAMLMapValue(root, "blacklist")
	if oldList == nil {
		return false, false
	}
	if oldList.Kind != yaml.SequenceNode {
		return false, true
	}
	blocklist := yamlEnsureMap(root, "blocklist", "黑名单命中后会拒绝挑战创建和下载授权签发。")
	static := yamlEnsureSequence(blocklist, "static",
		"由旧静态黑名单自动迁移；后续请直接维护 blocklist.static。")
	if static == nil {
		return false, true
	}
	for _, item := range oldList.Content {
		if item.Kind == yaml.ScalarNode && yamlSequenceHasScalar(static, item.Value) {
			continue
		}
		static.Content = append(static.Content, cloneYAMLNode(item))
	}
	yamlRemoveMapKey(root, "blacklist")
	return true, true
}
