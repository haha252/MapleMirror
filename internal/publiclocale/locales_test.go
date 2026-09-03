package publiclocale

import "testing"

func TestRegistryHasUniqueValidLocaleMetadata(t *testing.T) {
	locales := All()
	ids := map[string]bool{}
	prefixes := map[string]bool{}
	hrefLangs := map[string]bool{}
	defaults := 0
	for _, locale := range locales {
		if locale.ID == "" || locale.HrefLang == "" || locale.Script == "" {
			t.Fatalf("locale metadata incomplete: %+v", locale)
		}
		if ids[locale.ID] || prefixes[locale.PathPrefix] || hrefLangs[locale.HrefLang] {
			t.Fatalf("duplicate locale metadata: %+v", locales)
		}
		ids[locale.ID] = true
		prefixes[locale.PathPrefix] = true
		hrefLangs[locale.HrefLang] = true
		if locale.Default {
			defaults++
			if locale.PathPrefix != "" {
				t.Fatalf("default locale must not have URL prefix: %+v", locale)
			}
		} else if locale.PathPrefix == "" {
			t.Fatalf("non-default locale requires URL prefix: %+v", locale)
		}
	}
	if defaults != 1 {
		t.Fatalf("default locale count=%d want 1: %+v", defaults, locales)
	}
}

func TestRegistryPreservesCurrentPublicRoutes(t *testing.T) {
	if got := Default().ID; got != "zh-CN" {
		t.Fatalf("default locale=%q want zh-CN", got)
	}
	cases := []struct {
		locale string
		path   string
		want   string
	}{
		{"zh-CN", "/", "/"},
		{"zh-CN", "/about", "/about"},
		{"en", "/", "/en/"},
		{"en", "/about", "/en/about"},
		{"en", "/p1/", "/en/p1/"},
	}
	for _, test := range cases {
		if got := LocalizedPath(test.locale, test.path); got != test.want {
			t.Fatalf("LocalizedPath(%q, %q)=%q want %q", test.locale, test.path, got, test.want)
		}
	}
}

func TestEveryRegisteredLocaleExpandsLogicalPathOnce(t *testing.T) {
	locales := All()
	paths := Paths("/p1/")
	if len(paths) != len(locales) {
		t.Fatalf("paths=%v locales=%v", paths, locales)
	}
	seen := map[string]bool{}
	for i, locale := range locales {
		want := LocalizedPath(locale.ID, "/p1/")
		if paths[i] != want {
			t.Fatalf("path[%d]=%q want %q", i, paths[i], want)
		}
		if seen[paths[i]] {
			t.Fatalf("duplicate localized path %q from registry %+v", paths[i], locales)
		}
		seen[paths[i]] = true
	}
}

func TestRouteMapContainsEveryRegisteredLocale(t *testing.T) {
	routes := RouteMap("/about")
	for _, locale := range All() {
		if got := routes[locale.ID]; got != LocalizedPath(locale.ID, "/about") {
			t.Fatalf("route[%q]=%q", locale.ID, got)
		}
		if locale.HrefLang == "" || locale.Script == "" {
			t.Fatalf("locale metadata incomplete: %+v", locale)
		}
	}
}
