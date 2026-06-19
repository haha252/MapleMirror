package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectsLoadAssetPipelineClassifyRules(t *testing.T) {
	dir := t.TempDir()
	projectsPath := filepath.Join(dir, "projects.yaml")
	body := `projects:
  - id: sevenzip
    name: 7-Zip
    repository: ip7z/7zip
    enabled: true
    asset_pipeline:
      classify:
        rules:
          - match:
              regex: "^7z\\d+\\.exe$"
            assign:
              system: win
              architecture: amd64
              variant: binary
`
	if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := LoadProjects(projectsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	project := projects.Projects[0]
	if !project.PipelineUsesSystem() || !project.PipelineUsesArchitecture() {
		t.Fatalf("分类规则应启用系统和架构选择：%+v", project.AssetPipeline)
	}
	rule := project.AssetPipeline.Classify.Rules[0]
	if rule.Match.Regex == "" || rule.Assign.System != "win" ||
		rule.Assign.Architecture != "amd64" || rule.Assign.Variant != "binary" {
		t.Fatalf("分类规则解析错误：%+v", rule)
	}
}

func TestProjectsRejectInvalidAssetPipeline(t *testing.T) {
	cases := []string{
		"asset_pipeline:\n      classify:\n        rules:\n          - match: {}\n            assign:\n              system: win\n",
		"asset_pipeline:\n      classify:\n        rules:\n          - match:\n              regex: '('\n            assign:\n              system: win\n",
		"asset_pipeline:\n      classify:\n        rules:\n          - match:\n              exact: app.exe\n            assign:\n              architecture: sparc\n",
	}
	for _, extra := range cases {
		t.Run(extra, func(t *testing.T) {
			dir := t.TempDir()
			projectsPath := filepath.Join(dir, "projects.yaml")
			body := "projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n    " + extra
			if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProjects(projectsPath, nil); err == nil {
				t.Fatalf("应拒绝非法分类规则：%s", extra)
			}
		})
	}
}
