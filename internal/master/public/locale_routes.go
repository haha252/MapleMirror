package public

import (
	"net/http"
)

func (s Server) englishPagesHandler() http.Handler {
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
	return withPublicLocale(publicLocaleEN, http.StripPrefix("/en", pages))
}
