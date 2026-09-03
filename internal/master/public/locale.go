package public

import (
	"context"
	"net/http"
	"strings"
)

const (
	publicLocaleZH = "zh-CN"
	publicLocaleEN = "en"
)

type publicLocaleContextKey struct{}

type publicPageMeta struct {
	Title        string
	BrowserTitle string
	Description  string
	Subtitle     string
}

var englishPublicPageMeta = map[string]publicPageMeta{
	"/": {
		Title:        "Maple Mirror",
		BrowserTitle: "Maple Mirror - GitHub Release Software Downloads",
		Description:  "Maple Mirror is a public GitHub Release mirror offering free, stable and fast software downloads, with project search, release filtering, mirror node status, browser verification and a public API for scripts, clients, CI systems and automatic updaters.",
		Subtitle:     "A public GitHub Release mirror providing stable, fast software version and file downloads.",
	},
	"/stats": {
		Title:        "Statistics",
		BrowserTitle: "Maple Mirror Node Status and Download Statistics - Traffic, Visits and SLA",
		Description:  "View Maple Mirror visits, downloads, transferred traffic, mirror node availability and service SLA, including recent 30-day trends and public service health information for evaluating download availability and node stability.",
		Subtitle:     "View node status, visits, downloads, traffic and the last 30 days of trends.",
	},
	"/api-docs": {
		Title:        "API Docs",
		BrowserTitle: "Maple Mirror Public Download API Docs - Projects, Files and Automation",
		Description:  "Maple Mirror public API documentation for project and file discovery, browser downloads, automated downloads, proof-of-work verification, authorization tokens, CI integrations, clients and automatic updaters using GitHub Release mirror files.",
		Subtitle:     "Public download API documentation for web pages, scripts, clients and automatic updaters.",
	},
	"/changelog": {
		Title:        "Changelog",
		BrowserTitle: "Maple Mirror Changelog - Releases, Features and Maintenance",
		Description:  "Browse Maple Mirror releases, feature updates, maintenance records, service changes and important improvements covering mirror downloads, public APIs, node management, security policies and the public website experience.",
		Subtitle:     "Browse releases, feature updates, maintenance work and important changes over time.",
	},
	"/about": {
		Title:        "About this project",
		BrowserTitle: "About Maple Mirror - Public Service, Open Source and Sponsorship",
		Description:  "Learn about Maple Mirror's public-service goals, GitHub Release mirroring, open-source code, maintenance model, sponsorship and support channels, and how the project provides stable, transparent and reliable software downloads.",
		Subtitle:     "Learn about Maple Mirror's public mission, open-source project, maintenance and support.",
	},
}

func requestPublicLocale(r *http.Request) string {
	if r != nil {
		if locale, ok := r.Context().Value(publicLocaleContextKey{}).(string); ok && locale == publicLocaleEN {
			return publicLocaleEN
		}
	}
	return publicLocaleZH
}

func withPublicLocale(locale string, next http.Handler) http.Handler {
	if locale != publicLocaleEN {
		locale = publicLocaleZH
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), publicLocaleContextKey{}, locale)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func localizedPublicPath(locale, path string) string {
	if path == "" || !strings.HasPrefix(path, "/") {
		return path
	}
	if locale != publicLocaleEN {
		return path
	}
	if path == "/" {
		return "/en/"
	}
	return "/en" + path
}

func (s Server) localizeIndexablePage(r *http.Request, path string, data pageData) pageData {
	locale := requestPublicLocale(r)
	data.Lang = locale
	data.ZHPath = localizedPublicPath(publicLocaleZH, path)
	data.ENPath = localizedPublicPath(publicLocaleEN, path)
	data.CanonicalURL = s.canonicalURL(localizedPublicPath(locale, path))
	data.AlternateZHURL = s.canonicalURL(data.ZHPath)
	data.AlternateENURL = s.canonicalURL(data.ENPath)

	if locale == publicLocaleEN {
		if meta, ok := englishPublicPageMeta[path]; ok {
			data.Title = meta.Title
			data.BrowserTitle = meta.BrowserTitle
			data.Description = meta.Description
			data.Subtitle = meta.Subtitle
		} else if data.BodyClass == "page-project" {
			data.BrowserTitle = data.Title + " Releases and File Downloads - Maple Mirror"
			data.Description = projectMetaDescriptionEN(data.Title, "")
		}
	}
	return data
}

func projectMetaDescriptionEN(name, description string) string {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	base := name + " GitHub Release downloads on Maple Mirror, including the latest and historical releases, file lists, publication dates and current mirror availability."
	if description != "" {
		base = name + ": " + description + " Browse GitHub Release versions, files, publication dates and mirror download availability on Maple Mirror."
	}
	return truncateRunes(base, 160)
}
