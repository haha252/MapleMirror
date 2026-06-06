package adminui

import "testing"

func TestAdminPageForPathMapsTopNavigationPages(t *testing.T) {
	cases := map[string]string{
		"/admin/":         "overview",
		"/admin/nodes":    "nodes",
		"/admin/sync":     "sync",
		"/admin/projects": "projects",
		"/admin/security": "security",
	}
	for path, want := range cases {
		page, ok := adminPageForPath(path)
		if !ok || page.ID != want || page.Script == "" {
			t.Fatalf("page %s = %+v ok=%v, want %s", path, page, ok, want)
		}
	}
	if _, ok := adminPageForPath("/admin/missing"); ok {
		t.Fatal("unknown admin page should not resolve")
	}
}
