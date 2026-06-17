package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedExamplesMatchEmbeddedTemplates(t *testing.T) {
	cases := map[string][]byte{
		"config.example.yaml":   MasterExample,
		"projects.example.yaml": ProjectsExample,
		"quota.example.yaml":    QuotaExample,
		"node.example.yaml":     NodeExample,
	}
	for name, embedded := range cases {
		t.Run(name, func(t *testing.T) {
			published, err := os.ReadFile(filepath.Join("..", "..", "configs", name))
			if err != nil {
				t.Fatal(err)
			}
			if string(published) != string(embedded) {
				t.Fatalf("发布示例与嵌入模板不一致：%s", name)
			}
		})
	}
}

func TestExamplesLoadWithCurrentContract(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "config.yaml"), MasterExample)
	writeTestFile(t, filepath.Join(dir, "projects.yaml"), ProjectsExample)
	writeTestFile(t, filepath.Join(dir, "quota.yaml"), QuotaExample)
	writeTestFile(t, filepath.Join(dir, "node.yaml"), NodeExample)
	if _, err := LoadMaster(filepath.Join(dir, "config.yaml"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjects(filepath.Join(dir, "projects.yaml"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQuota(filepath.Join(dir, "quota.yaml"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNode(filepath.Join(dir, "node.yaml"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationTemplatesHaveChineseCommentsForEveryKey(t *testing.T) {
	cases := map[string][]byte{
		"config.example.yaml":   MasterExample,
		"projects.example.yaml": ProjectsExample,
		"projects.repair.yaml":  ProjectsRepairExample,
		"quota.example.yaml":    QuotaExample,
		"quota.repair.yaml":     QuotaRepairExample,
		"node.example.yaml":     NodeExample,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			assertEveryYAMLKeyHasChineseComment(t, data)
		})
	}
}

func TestExistingYAMLIsRepairedWithMissingKeysAndComments(t *testing.T) {
	dir := t.TempDir()
	masterPath := filepath.Join(dir, "config.yaml")
	quotaPath := filepath.Join(dir, "quota.yaml")
	projectPath := filepath.Join(dir, "projects.yaml")
	nodePath := filepath.Join(dir, "node.yaml")
	writeTestFile(t, masterPath, []byte("server:\n  public_listen: ':8080'\n"))
	writeTestFile(t, quotaPath, []byte("{}\n"))
	writeTestFile(t, projectPath, []byte("projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n"))
	writeTestFile(t, nodePath, []byte(`node:
  name: 节点一
server:
  listen: ":8081"
  public_download_base_url: "https://node1.example.com"
master:
  control_address: "https://127.0.0.1:9443"
`))
	if _, err := LoadMaster(masterPath, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQuota(quotaPath, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjects(projectPath, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNode(nodePath, nil); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, masterPath, "管理面板默认只监听本机")
	assertFileContains(t, quotaPath, "auto_ban_duration")
	assertFileContains(t, quotaPath, "请求次数额度桶配置")
	assertFileContains(t, projectPath, "资产入选规则；空列表保持默认允许所有资产")
	assertFileContains(t, nodePath, "旧版本下载令牌 Ed25519 公钥文件")
	projectContent := readTestFile(t, projectPath)
	if strings.Contains(projectContent, "project-icons/example.svg") ||
		strings.Contains(projectContent, "\\\\.zip$") {
		t.Fatalf("项目补齐模板不应注入示例图标或筛选规则：%s", projectContent)
	}
}

func TestRepairDoesNotOverwriteInvalidConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := []byte("server:\n  public_listen: invalid\n")
	writeTestFile(t, path, original)

	if _, err := LoadMaster(path, nil); err == nil {
		t.Fatal("无效配置应校验失败")
	}
	if got := []byte(readTestFile(t, path)); string(got) != string(original) {
		t.Fatalf("校验失败前不应写回 repair 结果：\n%s", got)
	}
}

func TestExistingYAMLFieldsAreRepairedWithMissingComments(t *testing.T) {
	dir := t.TempDir()
	quotaPath := filepath.Join(dir, "quota.yaml")
	projectPath := filepath.Join(dir, "projects.yaml")
	writeTestFile(t, quotaPath, []byte(`blocklist:
  feeds:
    - url: "https://example.com/all.txt"
`))
	writeTestFile(t, projectPath, []byte(`projects:
  - id: a
    name: 示例
    repository: owner/repo
    enabled: true
    asset_include:
      - pattern: "*.zip"
        type: glob
        required: false
`))
	if _, err := LoadQuota(quotaPath, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjects(projectPath, nil); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, quotaPath, "远程黑名单订阅源地址")
	assertFileContains(t, quotaPath, "订阅源刷新间隔")
	assertFileContains(t, projectPath, "pattern 匹配资产文件名")
	assertFileContains(t, projectPath, "required=false 表示可选入选规则")
}

func TestQuotaLegacyBlacklistMigratesIntoStaticBlocklist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quota.yaml")
	writeTestFile(t, path, []byte("blacklist:\n  - 192.0.2.0/24\nblocklist:\n  static:\n    - 198.51.100.1\n"))
	var warnings []string
	quota, err := LoadQuota(path, func(field, value string) {
		if replacement, ok := DeprecatedWarningMessage(value); ok {
			warnings = append(warnings, field+"=>"+replacement)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(quota.Blacklist) != 0 {
		t.Fatalf("旧 blacklist 应只作为兼容输入存在：%+v", quota.Blacklist)
	}
	if !containsString(quota.Blocklist.Static, "192.0.2.0/24") ||
		!containsString(quota.Blocklist.Static, "198.51.100.1") {
		t.Fatalf("旧 blacklist 未合并到 blocklist.static：%+v", quota.Blocklist.Static)
	}
	if !containsString(warnings, "quota.blacklist=>quota.blocklist.static") {
		t.Fatalf("旧 blacklist 应产生弃用警告：%+v", warnings)
	}
	content := readTestFile(t, path)
	if strings.Contains(content, "blacklist:") ||
		!strings.Contains(content, "192.0.2.0/24") ||
		!strings.Contains(content, "198.51.100.1") {
		t.Fatalf("旧 blacklist 应写回迁移到 blocklist.static：%s", content)
	}
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	if content := readTestFile(t, path); !strings.Contains(content, want) {
		t.Fatalf("%s 缺少补齐内容 %q：%s", path, want, content)
	}
}
