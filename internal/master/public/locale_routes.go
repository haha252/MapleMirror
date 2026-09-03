package public

import (
	"net/http"
	"strings"

	"mirror-server/internal/publiclocale"
)

func (s Server) localizedPagesHandler(locale publiclocale.Locale) http.Handler {
	pages := http.NewServeMux()
	pages.HandleFunc("/stats", s.statsPage)
	pages.HandleFunc("/changelog", s.changelogPage)
	pages.HandleFunc("/about", s.aboutPage)
	pages.HandleFunc("/api-docs", s.apiDocsPage)
	pages.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || singleProjectPath(r.URL.EscapedPath()) {
			s.downloadPage(w, r)
			return
		}
		http.NotFound(w, r)
	})

	prefix := "/" + strings.Trim(locale.PathPrefix, "/")
	return withPublicLocale(locale.ID, http.StripPrefix(prefix, pages))
}
