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
	page, ok := adminPageForPath("/admin/nodes/node-1/management")
	if !ok || page.ID != "node-management" {
		t.Fatalf("node management page = %+v ok=%v", page, ok)
	}
	if _, ok := adminPageForPath("/admin/missing"); ok {
		t.Fatal("unknown admin page should not resolve")
	}
}
