package public

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mirror-server/internal/publiclocale"
)

func TestIndexablePageLocaleMetadataFollowsRegistry(t *testing.T) {
	srv := Server{PublicBaseURL: "https://fyhub.cn"}
	for _, current := range publiclocale.All() {
		req := httptest.NewRequest(http.MethodGet, publiclocale.LocalizedPath(current.ID, "/about"), nil)
		req = req.WithContext(context.WithValue(req.Context(), publicLocaleContextKey{}, current.ID))
		data := srv.localizeIndexablePage(req, "/about", pageData{})

		if data.Lang != current.ID {
			t.Fatalf("page locale=%q want %q", data.Lang, current.ID)
		}
		if data.CanonicalURL != "https://fyhub.cn"+publiclocale.LocalizedPath(current.ID, "/about") {
			t.Fatalf("canonical=%q locale=%q", data.CanonicalURL, current.ID)
		}

		var routes map[string]string
		if err := json.Unmarshal([]byte(data.LocaleRoutesJSON), &routes); err != nil {
			t.Fatalf("locale routes JSON invalid: %v; %q", err, data.LocaleRoutesJSON)
		}
		if len(routes) != len(publiclocale.All()) || len(data.AlternateURLs) != len(publiclocale.All()) {
			t.Fatalf("registry metadata incomplete: routes=%v alternates=%v", routes, data.AlternateURLs)
		}
		for _, locale := range publiclocale.All() {
			wantPath := publiclocale.LocalizedPath(locale.ID, "/about")
			if routes[locale.ID] != wantPath {
				t.Fatalf("route[%q]=%q want %q", locale.ID, routes[locale.ID], wantPath)
			}
			wantURL := "https://fyhub.cn" + wantPath
			if !containsAlternate(data.AlternateURLs, locale.HrefLang, wantURL) {
				t.Fatalf("missing alternate %s %s: %+v", locale.HrefLang, wantURL, data.AlternateURLs)
			}
		}
	}
}

func TestNonDefaultLocalesHaveSSRPresentation(t *testing.T) {
	requiredPages := []string{"/", "/stats", "/api-docs", "/changelog", "/about"}
	for _, locale := range publiclocale.All() {
		if locale.Default {
			continue
		}
		presentation, ok := publicPresentationByLocale[locale.ID]
		if !ok || presentation.Project == nil {
			t.Fatalf("locale %q missing SSR presentation", locale.ID)
		}
		for _, path := range requiredPages {
			if _, ok := presentation.Pages[path]; !ok {
				t.Fatalf("locale %q missing SSR metadata for %q", locale.ID, path)
			}
		}
	}
}

func TestRegisteredLocaleRoutesAndScriptsAreServed(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, PublicBaseURL: "https://fyhub.cn"}
	handler := srv.Handler()

	for _, locale := range publiclocale.All() {
		if locale.Default {
			continue
		}
		path := publiclocale.LocalizedPath(locale.ID, "/")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html lang=\""+locale.ID+"\"") {
			t.Fatalf("localized route %q failed: %d %s", path, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, locale := range publiclocale.All() {
		if locale.Script != "" && !strings.Contains(rec.Body.String(), "/static/public/"+locale.Script) {
			t.Fatalf("registered locale script %q missing from page", locale.Script)
		}
	}
}

func containsAlternate(alternates []alternateURL, hrefLang, url string) bool {
	for _, alternate := range alternates {
		if alternate.HrefLang == hrefLang && alternate.URL == url {
			return true
		}
	}
	return false
}
