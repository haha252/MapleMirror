package public

import (
	"net/http"
)

func (s Server) Handler() http.Handler {
	if s.BlocklistExport == nil {
		s.BlocklistExport = newBlocklistExportCache(blocklistExportCacheTTL)
	}
	if s.StatsCache == nil {
		s.StatsCache = &statsCache{}
	}
	if s.WebVerifications == nil {
		s.WebVerifications = newWebVerificationTokenStore(webVerificationTokenCapacity, webVerificationTokenTTL)
	}
	mux := http.NewServeMux()
	if s.WebAssets != nil && s.WebAssets.staticFS != nil {
		mux.Handle("/static/public/", immutableCache(http.StripPrefix("/static/public/", http.FileServer(http.FS(s.WebAssets.staticFS)))))
	} else if s.WebAssets != nil {
		mux.Handle("/static/public/", immutableCache(http.StripPrefix("/static/public/", http.FileServer(http.Dir(s.WebAssets.staticDir)))))
	} else if assets, err := loadDefaultWebAssets(); err == nil {
		mux.Handle("/static/public/", immutableCache(http.StripPrefix("/static/public/", http.FileServer(http.FS(assets.staticFS)))))
	}
	mux.HandleFunc("/static/project-icons/", s.projectIcon)
	mux.HandleFunc("/downloads/", s.downloadMisrouted)
	mux.HandleFunc("/favicon.ico", s.favicon)
	mux.HandleFunc("/robots.txt", s.robotsTXT)
	mux.HandleFunc("/sitemap.xml", s.sitemap)
	mux.HandleFunc("/stats", s.statsPage)
	mux.HandleFunc("/about", s.aboutPage)
	mux.HandleFunc("/api-docs", s.apiDocsPage)
	mux.HandleFunc("/download/", s.downloadPowPage)
	mux.HandleFunc("/api/public/v1/blocklist.txt", s.blocklistTXT)
	mux.HandleFunc("/api/public/v1/blocklist.json", s.blocklistJSON)
	mux.HandleFunc("/api/public/v1/catalog", s.catalog)
	mux.Handle("/api/public/v1/stats", statsJSONCompression(http.HandlerFunc(s.statsAPI)))
	mux.Handle("/api/public/v1/stats/details", statsJSONCompression(http.HandlerFunc(s.statsDetailsAPI)))
	mux.HandleFunc("/api/public/v1/projects", s.projects)
	mux.HandleFunc("/api/public/v1/projects/", s.projectAssets)
	mux.HandleFunc("/api/public/v1/web/challenges", s.webChallenge)
	mux.HandleFunc("/api/public/v1/web/authorizations", s.webAuthorize)
	mux.HandleFunc("/api/public/v1/web/verifications", s.webVerification)
	mux.HandleFunc("/api/public/v1/api/challenges", s.apiChallenge)
	mux.HandleFunc("/api/public/v1/api/authorizations", s.apiAuthorize)
	mux.HandleFunc("/api/public/v1/authorizations/", s.authorization)
	mux.HandleFunc("/", s.downloadPage)
	if s.ResourceLimiter != nil {
		return s.ResourceLimiter.middleware(mux, s.TrustedCIDRs)
	}
	return mux
}

func (s Server) Close() {
	if s.ClientBlocks != nil {
		s.ClientBlocks.close()
	}
	if s.AbuseTracker != nil {
		s.AbuseTracker.close()
	}
	if s.WebVerifications != nil {
		s.WebVerifications.closeCleanup()
	}
}

func (s Server) ResetClientBlockCache(clientPrefix string) {
	if s.ClientBlocks != nil {
		s.ClientBlocks.invalidate(clientPrefix)
	}
}

func immutableCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}
