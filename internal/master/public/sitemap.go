package public

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"
)

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc string `xml:"loc"`
}

func (s Server) sitemap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	projects, err := s.Store.Projects(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "站点地图生成失败")
		return
	}
	origin := s.publicOrigin(r)
	paths := []string{"/", "/stats", "/api-docs", "/changelog", "/about"}
	for _, project := range projects {
		if strings.TrimSpace(project.ProjectID) == "" || strings.Contains(project.ProjectID, "/") {
			continue
		}
		paths = append(paths, "/"+url.PathEscape(project.ProjectID)+"/")
	}
	urls := make([]sitemapURL, 0, len(paths))
	for _, path := range paths {
		urls = append(urls, sitemapURL{Loc: origin + path})
	}
	body, err := xml.MarshalIndent(sitemapURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}, "", "  ")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "站点地图编码失败")
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n"))
}

func (s Server) robotsTXT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("User-agent: *\nAllow: /\nSitemap: " + s.publicOrigin(r) + "/sitemap.xml\n"))
}

func (s Server) publicOrigin(r *http.Request) string {
	if origin := strings.TrimRight(strings.TrimSpace(s.PublicBaseURL), "/"); origin != "" {
		return origin
	}
	return sitemapOrigin(r)
}

func sitemapOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Host)
	if forwardedHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = forwardedHost
	}
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}

func firstForwardedValue(value string) string {
	if value == "" {
		return ""
	}
	if head, _, ok := strings.Cut(value, ","); ok {
		value = head
	}
	return strings.ToLower(strings.TrimSpace(value))
}
