package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFooterIncludesProgramVersionBetweenAttributionAndContact(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	rec := httptest.NewRecorder()
	Server{Store: Store{DB: db}, Version: "91776e1"}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, body)
	}
	versionText := `<span class="site-footer__version">
      <svg aria-hidden="true"><use href="/static/public/icons.svg?v=`
	if !strings.Contains(body, versionText) {
		t.Fatalf("footer should include the injected program version: %s", body)
	}
	if !strings.Contains(body, `icons.svg?v=`) || !strings.Contains(body, `#git`) ||
		!strings.Contains(body, `data-i18n="footer.version">版本：</span>91776e1`) {
		t.Fatalf("footer should include the Git icon and short hash: %s", body)
	}
	builtAt := strings.Index(body, "by Frostlynx")
	versionAt := strings.Index(body, `class="site-footer__version"`)
	contactAt := strings.Index(body, `class="site-footer__contact"`)
	if builtAt < 0 || versionAt < 0 || contactAt < 0 || !(builtAt < versionAt && versionAt < contactAt) {
		t.Fatalf("footer version should be between attribution and contact: %s", body)
	}
}
