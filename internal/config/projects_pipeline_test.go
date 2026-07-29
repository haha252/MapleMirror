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
      selectors:
        architecture_enabled: false
        system_enabled: true
      classify:
        mode: rules
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
	if project.ArchitectureSelectorEnabled() || !project.SystemSelectorEnabled() {
		t.Fatalf("选择器开关应独立于分类规则：%+v", project.AssetPipeline.Selectors)
	}
	if project.AssetPipeline.Classify.Mode != "rules" {
		t.Fatalf("分类模式解析错误：%q", project.AssetPipeline.Classify.Mode)
	}
	rule := project.AssetPipeline.Classify.Rules[0]
	if rule.Match.Regex == "" || rule.Assign.System != "win" ||
		rule.Assign.Architecture != "amd64" || rule.Assign.Variant != "binary" {
		t.Fatalf("分类规则解析错误：%+v", rule)
	}
}

func TestProjectClassifyModeSeparatesRegexAndRules(t *testing.T) {
	rule := AssetClassifyRule{Match: AssetClassifyMatch{Exact: "tool.exe"},
		Assign: AssetClassification{System: "win", Architecture: "amd64"}}
	regexProject := Project{
		AssetPipeline: AssetPipeline{Classify: AssetClassifyConfig{
			Mode: "regex",
			Regex: AssetClassifyRegexConfig{
				ArchitectureMatchEnabled: true,
				ArchitectureRegex:        "(amd64)",
			},
			Rules: []AssetClassifyRule{rule},
		}},
	}
	if !regexProject.PipelineUsesArchitecture() || regexProject.PipelineUsesSystem() {
		t.Fatalf("regex 模式只应使用旧正则开关：%+v", regexProject)
	}
	rulesProject := Project{
		SystemMatchEnabled: true,
		AssetPipeline: AssetPipeline{Classify: AssetClassifyConfig{
			Mode: "rules", Rules: []AssetClassifyRule{rule},
		}},
	}
	if !rulesProject.PipelineUsesArchitecture() || !rulesProject.PipelineUsesSystem() {
		t.Fatalf("rules 模式应只由分类规则启用选择器：%+v", rulesProject)
	}
}

func TestProjectSelectorSwitchesOverrideClassifierMode(t *testing.T) {
	enabled := true
	disabled := false
	project := Project{AssetPipeline: AssetPipeline{
		Selectors: AssetSelectorConfig{
			ArchitectureEnabled: &disabled,
			SystemEnabled:       &enabled,
		},
		Classify: AssetClassifyConfig{
			Mode: "rules",
			Rules: []AssetClassifyRule{{
				Match:  AssetClassifyMatch{Exact: "tool.zip"},
				Assign: AssetClassification{System: "linux", Architecture: "amd64"},
			}},
		},
	}}
	if project.ArchitectureSelectorEnabled() {
		t.Fatal("架构选择器显式关闭后不应被 rules 分类自动开启")
	}
	if !project.SystemSelectorEnabled() {
		t.Fatal("系统选择器应可在 rules 分类下独立开启")
	}
	if !project.PipelineUsesArchitecture() || !project.PipelineUsesSystem() {
		t.Fatal("展示开关不应改变资产分类字段")
	}
}

func TestProjectSelectorSwitchesFallbackToClassifierForCompatibility(t *testing.T) {
	project := Project{AssetPipeline: AssetPipeline{Classify: AssetClassifyConfig{
		Mode: "rules",
		Rules: []AssetClassifyRule{{
			Match:  AssetClassifyMatch{Exact: "tool.zip"},
			Assign: AssetClassification{Architecture: "amd64"},
		}},
	}}}
	if !project.ArchitectureSelectorEnabled() || project.SystemSelectorEnabled() {
		t.Fatal("未配置展示开关时应保持旧版按分类字段推断的行为")
	}
}

func TestProjectClassifyRegexConfigFallsBackToLegacyFields(t *testing.T) {
	project := Project{
		ArchitectureMatchEnabled: true,
		ArchitectureRegex:        "(amd64)",
		SystemMatchEnabled:       true,
		SystemRegex:              "(linux)",
		AssetPipeline: AssetPipeline{Classify: AssetClassifyConfig{
			Mode: "regex",
		}},
	}
	if !project.ClassifyArchitectureEnabled() ||
		project.ClassifyArchitectureRegex() != "(amd64)" ||
		!project.ClassifySystemEnabled() ||
		project.ClassifySystemRegex() != "(linux)" {
		t.Fatalf("旧顶层正则配置应作为兼容回退：%+v", project)
	}
}

func TestProjectsLoadNestedClassifyRegexConfig(t *testing.T) {
	dir := t.TempDir()
	projectsPath := filepath.Join(dir, "projects.yaml")
	body := `projects:
  - id: app
    name: App
    repository: owner/app
    enabled: true
    asset_pipeline:
      classify:
        mode: regex
        regex:
          architecture_match_enabled: true
          architecture_regex: "(amd64)"
          system_match_enabled: true
          system_regex: "(linux)"
`
	if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := LoadProjects(projectsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	project := projects.Projects[0]
	if !project.PipelineUsesArchitecture() || !project.PipelineUsesSystem() {
		t.Fatalf("嵌套 regex 配置应启用系统和架构选择：%+v", project.AssetPipeline)
	}
	if project.ClassifyArchitectureRegex() != "(amd64)" || project.ClassifySystemRegex() != "(linux)" {
		t.Fatalf("嵌套 regex 配置解析错误：%+v", project.AssetPipeline.Classify.Regex)
	}
}

func TestNormalizeAssetArchitectureKeepsAndroidABI(t *testing.T) {
	cases := []string{"arm64-v8a", "armeabi-v7a", "x86_64"}
	for _, value := range cases {
		got, ok := NormalizeAssetArchitecture(value)
		if !ok || got != value {
			t.Fatalf("Android ABI 架构应保留原名 value=%q got=%q ok=%v", value, got, ok)
		}
	}
}

func TestNormalizeAssetSystemSupportsHarmony(t *testing.T) {
	for _, value := range []string{"harmony", "HarmonyOS", "OpenHarmony", "OHOS"} {
		got, ok := NormalizeAssetSystem(value)
		if !ok || got != "harmony" {
			t.Fatalf("鸿蒙系统别名应规范化为 harmony value=%q got=%q ok=%v", value, got, ok)
		}
	}
}

func TestProjectsRejectInvalidAssetPipeline(t *testing.T) {
	cases := []string{
		"asset_pipeline:\n      classify:\n        mode: mixed\n",
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
