package mirrorsync

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestScanClassifiesSevenZipAssetsWithAssignmentRules(t *testing.T) {
	db := openScannerTestDB(t)
	defer db.Close()
	seedNode(t, db)
	good := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	assets := []GitHubAsset{
		{ID: 1, Name: "7z2601.exe", Size: 10, URL: "https://example.invalid/win", Digest: good},
		{ID: 2, Name: "7z2601-arm.exe", Size: 10, URL: "https://example.invalid/arm", Digest: good},
		{ID: 3, Name: "7z2601-arm64.exe", Size: 10, URL: "https://example.invalid/arm64", Digest: good},
		{ID: 4, Name: "7z2601-linux-x64.tar.xz", Size: 10, URL: "https://example.invalid/linux64", Digest: good},
		{ID: 5, Name: "7z2601-linux-x86.tar.xz", Size: 10, URL: "https://example.invalid/linux86", Digest: good},
		{ID: 6, Name: "7z2601-mac.tar.xz", Size: 10, URL: "https://example.invalid/mac", Digest: good},
		{ID: 7, Name: "7z2601-src.tar.xz", Size: 10, URL: "https://example.invalid/src", Digest: good},
	}
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: []GitHubRelease{{
		ID: 1, TagName: "26.01", PublishedAt: time.Now(), Assets: assets,
	}}}}
	projects := config.Projects{Projects: []config.Project{sevenZipProject()}}
	summary, err := scanner.Scan(context.Background(), projects, "", "req-scan")
	if err != nil {
		t.Fatal(err)
	}
	if summary.AcceptedAssets != 6 || summary.RejectedAssets != 1 {
		t.Fatalf("7-Zip 分类统计错误 accepted=%d rejected=%d", summary.AcceptedAssets, summary.RejectedAssets)
	}
	assertAssetClass(t, db, "sevenzip:1:1", "win", "amd64", "binary")
	assertAssetClass(t, db, "sevenzip:1:2", "win", "arm", "binary")
	assertAssetClass(t, db, "sevenzip:1:3", "win", "arm64", "binary")
	assertAssetClass(t, db, "sevenzip:1:4", "linux", "amd64", "binary")
	assertAssetClass(t, db, "sevenzip:1:5", "linux", "x86", "binary")
	assertAssetClass(t, db, "sevenzip:1:6", "darwin", "amd64", "binary")
	assertCount(t, db, "assets", 6)
}

func TestClassifyRulesApplyInOrder(t *testing.T) {
	classifier, err := newAssetClassifier(config.Project{
		AssetPipeline: config.AssetPipeline{Classify: config.AssetClassifyConfig{
			Rules: []config.AssetClassifyRule{
				{Match: config.AssetClassifyMatch{Glob: "*.exe"},
					Assign: config.AssetClassification{System: "win", Architecture: "amd64"}},
				{Match: config.AssetClassifyMatch{Exact: "tool-arm.exe"},
					Assign: config.AssetClassification{Architecture: "arm"}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := classifier.Classify(ResourceCandidate{FileName: "tool-arm.exe"}, ResourceVersion{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.System != "win" || got.Architecture != "arm" {
		t.Fatalf("后续规则应覆盖前序赋值：+%v", got)
	}
}

func TestClassifyModeRegexIgnoresRules(t *testing.T) {
	classifier, err := newAssetClassifier(config.Project{
		AssetPipeline: config.AssetPipeline{Classify: config.AssetClassifyConfig{
			Mode: "regex",
			Regex: config.AssetClassifyRegexConfig{
				ArchitectureMatchEnabled: true,
				ArchitectureRegex:        `(amd64)`,
			},
			Rules: []config.AssetClassifyRule{{
				Match:  config.AssetClassifyMatch{Regex: "("},
				Assign: config.AssetClassification{System: "win", Architecture: "arm64"},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := classifier.Classify(ResourceCandidate{FileName: "tool-amd64.zip"}, ResourceVersion{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Architecture != "amd64" || got.System != "" {
		t.Fatalf("regex 模式不应执行 rules：+%v", got)
	}
}

func TestClassifyModeRulesIgnoresRegex(t *testing.T) {
	classifier, err := newAssetClassifier(config.Project{
		AssetPipeline: config.AssetPipeline{Classify: config.AssetClassifyConfig{
			Mode: "rules",
			Regex: config.AssetClassifyRegexConfig{
				ArchitectureMatchEnabled: true,
				ArchitectureRegex:        "(",
				SystemMatchEnabled:       true,
				SystemRegex:              "(",
			},
			Rules: []config.AssetClassifyRule{{
				Match: config.AssetClassifyMatch{Exact: "tool.exe"},
				Assign: config.AssetClassification{
					System: "win", Architecture: "amd64",
				},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := classifier.Classify(ResourceCandidate{FileName: "tool.exe"}, ResourceVersion{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.System != "win" || got.Architecture != "amd64" {
		t.Fatalf("rules 模式不应编译或执行旧 regex：+%v", got)
	}
}

func TestStarlarkClassifierAssignsAssetFields(t *testing.T) {
	classifier, err := newAssetClassifier(config.Project{
		AssetPipeline: config.AssetPipeline{Classify: config.AssetClassifyConfig{
			Script: config.AssetClassifyScript{Inline: `
def classify(asset, release, project, batch):
    if asset["file_name"] == "tool.exe":
        return {"system": "win", "architecture": "amd64", "variant": "scripted"}
    return None
`},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := classifier.Classify(ResourceCandidate{FileName: "tool.exe"}, ResourceVersion{Version: "v1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.System != "win" || got.Architecture != "amd64" || got.Variant != "scripted" {
		t.Fatalf("Starlark 分类结果错误：+%v", got)
	}
}

func TestStarlarkClassifierRejectsInvalidReturn(t *testing.T) {
	classifier, err := newAssetClassifier(config.Project{
		AssetPipeline: config.AssetPipeline{Classify: config.AssetClassifyConfig{
			Script: config.AssetClassifyScript{Inline: `def classify(asset, release, project, batch):
    return "bad"
`},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := classifier.Classify(ResourceCandidate{FileName: "tool.exe"}, ResourceVersion{}, nil); err == nil {
		t.Fatal("Starlark 返回非 dict 时应失败")
	}
}

func TestStarlarkClassifierRejectsInvalidFieldType(t *testing.T) {
	classifier, err := newAssetClassifier(config.Project{
		AssetPipeline: config.AssetPipeline{Classify: config.AssetClassifyConfig{
			Script: config.AssetClassifyScript{Inline: `def classify(asset, release, project, batch):
    return {"system": 1}
`},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := classifier.Classify(ResourceCandidate{FileName: "tool.exe"}, ResourceVersion{}, nil); err == nil {
		t.Fatal("Starlark 返回非字符串 system 时应失败")
	}
}

func TestClassificationReasonKeepsStarlarkMarker(t *testing.T) {
	classifier, err := newAssetClassifier(config.Project{
		AssetPipeline: config.AssetPipeline{Classify: config.AssetClassifyConfig{
			Rules: []config.AssetClassifyRule{{
				Match:  config.AssetClassifyMatch{Exact: "tool.exe"},
				Assign: config.AssetClassification{System: "win"},
			}},
			Script: config.AssetClassifyScript{Inline: `def classify(asset, release, project, batch):
    return {"architecture": "amd64"}
`},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := classifier.Classify(ResourceCandidate{FileName: "tool.exe"}, ResourceVersion{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClassificationReason != "rule:1,starlark" {
		t.Fatalf("分类原因应保留 Starlark 标记：%q", got.ClassificationReason)
	}
}

func sevenZipProject() config.Project {
	return config.Project{
		ID: "sevenzip", Name: "7-Zip", Repository: "ip7z/7zip", Enabled: true,
		RetainVersions: 1,
		AssetInclude:   config.AssetRules{{Pattern: `^7z\d+.*\.(exe|7z|tar\.xz)$`, Type: "regex"}},
		AssetExclude:   config.AssetRules{{Pattern: `(?i)src`, Type: "regex"}},
		AssetPipeline: config.AssetPipeline{Classify: config.AssetClassifyConfig{
			Rules: []config.AssetClassifyRule{
				classifyRule(`^7z\d+\.exe$`, "win", "amd64"),
				classifyRule(`^7z\d+-arm\.exe$`, "win", "arm"),
				classifyRule(`^7z\d+-arm64\.exe$`, "win", "arm64"),
				classifyRule(`^7z\d+-linux-x64\.tar\.xz$`, "linux", "amd64"),
				classifyRule(`^7z\d+-linux-x86\.tar\.xz$`, "linux", "x86"),
				classifyRule(`^7z\d+-mac\.tar\.xz$`, "darwin", "amd64"),
			},
		}},
	}
}

func classifyRule(pattern, system, arch string) config.AssetClassifyRule {
	return config.AssetClassifyRule{
		Match: config.AssetClassifyMatch{Regex: pattern},
		Assign: config.AssetClassification{
			System: system, Architecture: arch, Variant: "binary",
		},
	}
}

func assertAssetClass(t *testing.T, db *sql.DB, assetID, system, arch, variant string) {
	t.Helper()
	var gotSystem, gotArch, gotVariant string
	err := db.QueryRow(`SELECT system, architecture, variant FROM assets WHERE id = ?`, assetID).
		Scan(&gotSystem, &gotArch, &gotVariant)
	if err != nil || gotSystem != system || gotArch != arch || gotVariant != variant {
		t.Fatalf("资产分类错误 asset=%s got=%q/%q/%q want=%q/%q/%q err=%v",
			assetID, gotSystem, gotArch, gotVariant, system, arch, variant, err)
	}
}
