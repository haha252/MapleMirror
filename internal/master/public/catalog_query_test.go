package public

import (
	"net/url"
	"strings"
	"testing"

	"mirror-server/internal/config"
)

func TestCatalogQuerySearchesIDNameTagsAndLabels(t *testing.T) {
	index := testCatalogIndex()
	cases := map[string][]string{
		"ALPHA":   {"alpha"},
		"启动":      {"alpha"},
		"FFMPEG":  {"alpha"},
		"Windows": {"beta", "alpha"},
	}
	for query, want := range cases {
		result, err := index.resolve(url.Values{"q": {query}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(result.Projects, ",") != strings.Join(want, ",") {
			t.Fatalf("q=%q projects=%v want %v", query, result.Projects, want)
		}
	}
}

func TestCatalogQuerySplitsExactAndPartialTagMatches(t *testing.T) {
	index := testCatalogIndex()
	result, err := index.resolve(url.Values{"filter": {
		"software_type:launcher", "supported_system:linux",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Projects, ",") != "alpha" {
		t.Fatalf("严格匹配错误：%v", result.Projects)
	}
	if strings.Join(result.Suggestions, ",") != "beta,gamma" {
		t.Fatalf("相似推荐应按命中数和名称排序：%v", result.Suggestions)
	}
}

func TestCatalogQueryKeepsSearchAsHardCondition(t *testing.T) {
	index := testCatalogIndex()
	result, err := index.resolve(url.Values{
		"q":      {"Beta"},
		"filter": {"supported_system:linux"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Projects) != 0 || len(result.Suggestions) != 0 {
		t.Fatalf("未命中标签的搜索项目不应展示：%+v", result)
	}
}

func TestCatalogQueryUsesFuzzyFallbackOnlyWhenDirectSearchIsEmpty(t *testing.T) {
	index := testCatalogIndex()
	result, err := index.resolve(url.Values{"q": {"ffmepg"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Projects) != 0 || len(result.Suggestions) != 1 ||
		result.Suggestions[0] != "alpha" {
		t.Fatalf("拼写兜底结果错误：projects=%v suggestions=%v",
			result.Projects, result.Suggestions)
	}
}

func TestCatalogQueryCapsFuzzySuggestionsAtThree(t *testing.T) {
	filters := testCatalogFilters()
	var projects []config.Project
	for _, id := range []string{"mode", "mody", "modz", "modq"} {
		projects = append(projects, config.Project{
			ID: id, Name: id, Enabled: true,
		})
	}
	index := &catalogIndex{snapshot: newCatalogSnapshot(
		config.Projects{Projects: projects}, filters, 1)}
	result, err := index.resolve(url.Values{"q": {"modx"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Suggestions) != 3 {
		t.Fatalf("模糊推荐数量=%d want 3：%v", len(result.Suggestions), result.Suggestions)
	}
}

func TestCatalogQueryRejectsUnknownAndOversizedConditions(t *testing.T) {
	index := testCatalogIndex()
	for _, values := range []url.Values{
		{"filter": {"missing:value"}},
		{"filter": {"software_type:missing"}},
		{"q": {strings.Repeat("搜", 101)}},
	} {
		if _, err := index.resolve(values); err != errInvalidCatalogQuery {
			t.Fatalf("应拒绝目录条件 %v：%v", values, err)
		}
	}
}

func testCatalogIndex() *catalogIndex {
	projects := config.Projects{Projects: []config.Project{
		{ID: "alpha", Name: "FFmpeg 启动器", Enabled: true, Tags: map[string][]string{
			"software_type": {"launcher"}, "supported_system": {"windows", "linux"},
			"keywords": {"ffmpeg"},
		}},
		{ID: "beta", Name: "Beta 工具", Enabled: true, Tags: map[string][]string{
			"software_type": {"launcher"}, "supported_system": {"windows"},
		}},
		{ID: "gamma", Name: "Gamma 工具", Enabled: true, Tags: map[string][]string{
			"supported_system": {"linux"},
		}},
	}}
	return &catalogIndex{snapshot: newCatalogSnapshot(projects, testCatalogFilters(), 1)}
}

func testCatalogFilters() config.Filters {
	return config.Filters{Selectors: []config.FilterSelector{
		{ID: "software_type", Name: "软件类型", Options: []config.FilterOption{
			{ID: "launcher", Name: "启动器"},
		}},
		{ID: "supported_system", Name: "支持系统", Options: []config.FilterOption{
			{ID: "windows", Name: "Windows"}, {ID: "linux", Name: "Linux"},
		}},
	}}
}
