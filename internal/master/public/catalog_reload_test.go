package public

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestCatalogIndexReloadsChangedSplitFilesAndClearsCache(t *testing.T) {
	dir := t.TempDir()
	projectsPath := filepath.Join(dir, "projects.yaml")
	filtersPath := filepath.Join(dir, "filters.yaml")
	projectDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCatalogTestFile(t, projectsPath, "project_files:\n  - projects/*.yaml\n")
	writeCatalogProject(t, filepath.Join(projectDir, "one.yaml"), "one", "项目一")
	writeCatalogTestFile(t, filtersPath, "cache:\n  max_size: 1 KiB\nselectors: []\n")
	projects, err := config.LoadProjects(projectsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	filters, err := config.LoadFilters(filtersPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	cache := newCatalogResultCache(filters.CacheBytes)
	defer cache.close()
	cache.put("old", catalogPayload{Body: []byte("old")})
	index := &catalogIndex{
		snapshot:     newCatalogSnapshot(projects, filters, 1),
		projectsPath: projectsPath, filtersPath: filtersPath, cache: cache,
	}
	index.observed = catalogSourceFingerprint(projectsPath, filtersPath, projects.ProjectFiles)

	secondPath := filepath.Join(projectDir, "two.yaml")
	writeCatalogProject(t, secondPath, "two", "项目二")
	touchCatalogTestFile(t, secondPath)
	if !index.reloadIfChanged() {
		t.Fatal("新增 glob 项目文件应触发索引重建")
	}
	if index.current().Generation != 2 || len(index.current().Projects) != 2 {
		t.Fatalf("热加载快照错误：%+v", index.current())
	}
	if len(cache.entries) != 0 {
		t.Fatal("索引换代应清空查询缓存")
	}
	if index.reloadIfChanged() {
		t.Fatal("文件未变化时不应重建索引")
	}
	if err := os.Remove(secondPath); err != nil {
		t.Fatal(err)
	}
	if !index.reloadIfChanged() || index.current().Generation != 3 ||
		len(index.current().Projects) != 1 {
		t.Fatalf("删除 glob 项目文件应重建索引：%+v", index.current())
	}
}

func TestCatalogIndexKeepsLastGoodSnapshotOnInvalidReload(t *testing.T) {
	dir := t.TempDir()
	projectsPath := filepath.Join(dir, "projects.yaml")
	filtersPath := filepath.Join(dir, "filters.yaml")
	writeCatalogTestFile(t, projectsPath, "projects:\n  - id: one\n    name: 项目一\n"+
		"    repository: owner/one\n    enabled: true\n")
	writeCatalogTestFile(t, filtersPath, "cache:\n  max_size: 1 KiB\nselectors: []\n")
	projects, _ := config.LoadProjects(projectsPath, nil)
	filters, _ := config.LoadFilters(filtersPath, nil)
	index := &catalogIndex{
		snapshot:     newCatalogSnapshot(projects, filters, 4),
		projectsPath: projectsPath, filtersPath: filtersPath,
	}
	index.observed = catalogSourceFingerprint(projectsPath, filtersPath, projects.ProjectFiles)
	writeCatalogTestFile(t, filtersPath, "cache:\n  max_size: 1 KiB\nselectors:\n  - id: Bad\n    name: 错误\n")
	touchCatalogTestFile(t, filtersPath)
	if index.reloadIfChanged() || index.current().Generation != 4 {
		t.Fatalf("非法配置不应替换有效快照：%+v", index.current())
	}
	if index.reloadIfChanged() {
		t.Fatal("同一份非法配置不应反复尝试重建")
	}
}

func writeCatalogProject(t *testing.T, path, id, name string) {
	t.Helper()
	writeCatalogTestFile(t, path, "id: "+id+"\nname: "+name+
		"\nrepository: owner/"+id+"\nenabled: true\ntags:\n  keywords: ["+id+"]\n")
}

func writeCatalogTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func touchCatalogTestFile(t *testing.T, path string) {
	t.Helper()
	next := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, next, next); err != nil {
		t.Fatal(err)
	}
}
