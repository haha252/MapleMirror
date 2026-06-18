package public

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSponsorsFromFilesUsesFirstExistingFile(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "sponsor.json")
	second := filepath.Join(dir, "legacy.json")
	writeSponsorFile(t, first, `[{"name":"新位置","date":"2026-02-01","amount":"¥2"}]`)
	writeSponsorFile(t, second, `[{"name":"旧位置","date":"2026-01-01","amount":"¥1"}]`)

	sponsors := loadSponsorsFromFiles([]string{filepath.Join(dir, "missing.json"), first, second})

	if len(sponsors) != 1 || sponsors[0].Name != "新位置" {
		t.Fatalf("expected first existing sponsor file, got %#v", sponsors)
	}
}

func TestLoadSponsorsFromFilesFallsBackAndSorts(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "sponsors.json")
	writeSponsorFile(t, legacy, `[
		{"name":"普通","date":"2026-03-01","amount":"¥3"},
		{"name":"置顶","date":"2026-01-01","amount":"¥1","pinned":true},
		{"name":"较新","date":"2026-04-01","amount":"¥4"}
	]`)

	sponsors := loadSponsorsFromFiles([]string{filepath.Join(dir, "sponsor.json"), legacy})

	if len(sponsors) != 3 {
		t.Fatalf("sponsor count=%d want 3", len(sponsors))
	}
	got := []string{sponsors[0].Name, sponsors[1].Name, sponsors[2].Name}
	want := []string{"置顶", "较新", "普通"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order=%v want %v", got, want)
		}
	}
}

func writeSponsorFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
