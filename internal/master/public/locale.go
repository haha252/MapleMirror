package public

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"mirror-server/internal/publiclocale"
)

type publicLocaleContextKey struct{}

type publicPageMeta struct {
	Title        string
	BrowserTitle string
	Description  string
	Subtitle     string
}

type alternateURL struct {
	HrefLang string
	URL      string
}

type publicLocalePresentation struct {
	Pages   map[string]publicPageMeta
	Project func(name, description string) (browserTitle, metaDescription string)
}

var publicPresentationByLocale = map[string]publicLocalePresentation{
	"en": {
		Pages: map[string]publicPageMeta{
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
		},
		Project: englishProjectPageMeta,
	},
}

func requestPublicLocale(r *http.Request) string {
	if r != nil {
		if locale, ok := r.Context().Value(publicLocaleContextKey{}).(string); ok {
			if _, supported := publiclocale.Lookup(locale); supported {
				return locale
			}
		}
	}
	return publiclocale.Default().ID
}

func withPublicLocale(locale string, next http.Handler) http.Handler {
	if _, ok := publiclocale.Lookup(locale); !ok {
		locale = publiclocale.Default().ID
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), publicLocaleContextKey{}, locale)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s Server) localizeIndexablePage(r *http.Request, path string, data pageData) pageData {
	locale := requestPublicLocale(r)
	data.Lang = locale
	data.CanonicalURL = s.canonicalURL(publiclocale.LocalizedPath(locale, path))

	routes := publiclocale.RouteMap(path)
	if encoded, err := json.Marshal(routes); err == nil {
		data.LocaleRoutesJSON = string(encoded)
	}

	locales := publiclocale.All()
	data.AlternateURLs = make([]alternateURL, 0, len(locales))
	for _, item := range locales {
		url := s.canonicalURL(publiclocale.LocalizedPath(item.ID, path))
		data.AlternateURLs = append(data.AlternateURLs, alternateURL{HrefLang: item.HrefLang, URL: url})
		if item.Default {
			data.DefaultAlternateURL = url
		}
	}

	if presentation, ok := publicPresentationByLocale[locale]; ok {
		if meta, exists := presentation.Pages[path]; exists {
			data.Title = meta.Title
			data.BrowserTitle = meta.BrowserTitle
			data.Description = meta.Description
			data.Subtitle = meta.Subtitle
		} else if data.BodyClass == "page-project" && presentation.Project != nil {
			data.BrowserTitle, data.Description = presentation.Project(data.Title, "")
		}
	}
	return data
}

func englishProjectPageMeta(name, description string) (string, string) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	metaDescription := name + " GitHub Release downloads on Maple Mirror, including the latest and historical releases, file lists, publication dates and current mirror availability."
	if description != "" {
		metaDescription = name + ": " + description + " Browse GitHub Release versions, files, publication dates and mirror download availability on Maple Mirror."
	}
	return name + " Releases and File Downloads - Maple Mirror", truncateRunes(metaDescription, 160)
}
