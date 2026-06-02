package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectsLoadAssetRules(t *testing.T) {
	dir := t.TempDir()
	projectsPath := filepath.Join(dir, "projects.yaml")
	body := `projects:
  - id: a
    name: 示例
    repository: owner/repo
    enabled: true
    architecture_regex: '(amd64)'
    asset_include:
      - "*.apk"
      - pattern: "\\.zip$"
        type: regex
        required: true
    asset_exclude:
      - pattern: "debug"
        type: regex
`
	if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := LoadProjects(projectsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	include := projects.Projects[0].AssetInclude
	if len(include) != 2 || include[0].Pattern != "*.apk" || include[0].Type != "glob" ||
		include[1].Pattern != `\.zip$` || include[1].Type != "regex" || !include[1].Required {
		t.Fatalf("资产包含规则解析错误：%+v", include)
	}
	exclude := projects.Projects[0].AssetExclude
	if len(exclude) != 1 || exclude[0].Pattern != "debug" || exclude[0].Type != "regex" {
		t.Fatalf("资产排除规则解析错误：%+v", exclude)
	}
}

func TestProjectsRejectInvalidAssetRules(t *testing.T) {
	cases := []string{
		"asset_include:\n      - pattern: ''\n        type: regex\n",
		"asset_include:\n      - pattern: '('\n        type: regex\n",
		"asset_include:\n      - pattern: '*.apk'\n        type: wildcard\n",
	}
	for _, extra := range cases {
		t.Run(extra, func(t *testing.T) {
			dir := t.TempDir()
			projectsPath := filepath.Join(dir, "projects.yaml")
			body := "projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n    architecture_regex: '(amd64)'\n    " + extra
			if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProjects(projectsPath, nil); err == nil {
				t.Fatalf("应拒绝非法资产规则：%s", extra)
			}
		})
	}
}
