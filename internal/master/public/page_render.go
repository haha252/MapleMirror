package public

import (
	"bytes"
	"html/template"
	"net/http"

	"mirror-server/internal/publiclocale"
)

type pageData struct {
	Title               string
	BrowserTitle        string
	Subtitle            string
	BeforeNotices       []noticeView
	AfterNotices        []noticeView
	Description         string
	CanonicalURL        string
	AlternateURLs       []alternateURL
	DefaultAlternateURL string
	LocaleRoutesJSON    string
	LocalePathPrefix    string
	LocaleScripts       []string
	DefaultLocale       string
	Lang                string
	Robots              string
	BodyClass           string
	HideHeader          bool
	Version             string
	CatalogSearch       bool
	ChangelogSearch     bool
	NavHomePath         string
	NavStatsPath        string
	NavAPIDocsPath      string
	NavChangelogPath    string
	NavAboutPath        string
	Body                template.HTML
	Styles              []string
	Scripts             []string
	StaticNames         []string
	StaticJSON          template.JS
	StatusCode          int
}

func (s Server) assets() (*webAssets, error) {
	if s.WebAssets != nil {
		return s.WebAssets, nil
	}
	return loadDefaultWebAssets()
}

func (s Server) renderPage(w http.ResponseWriter, data pageData) {
	assets, err := s.assets()
	if err != nil {
		http.Error(w, "页面模板读取失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	data.StaticJSON = assets.staticJSONFor(data.StaticNames)
	data.DefaultLocale = publiclocale.Default().ID
	if _, ok := publiclocale.Lookup(data.Lang); !ok {
		data.Lang = data.DefaultLocale
	}
	if len(data.LocaleScripts) == 0 {
		for _, locale := range publiclocale.All() {
			if locale.Script != "" {
				data.LocaleScripts = append(data.LocaleScripts, locale.Script)
			}
		}
	}
	if locale, ok := publiclocale.Lookup(data.Lang); ok {
		data.LocalePathPrefix = locale.PathPrefix
	}
	data.NavHomePath = publiclocale.LocalizedPath(data.Lang, "/")
	data.NavStatsPath = publiclocale.LocalizedPath(data.Lang, "/stats")
	data.NavAPIDocsPath = publiclocale.LocalizedPath(data.Lang, "/api-docs")
	data.NavChangelogPath = publiclocale.LocalizedPath(data.Lang, "/changelog")
	data.NavAboutPath = publiclocale.LocalizedPath(data.Lang, "/about")
	if data.StatusCode == 0 {
		data.StatusCode = http.StatusOK
	}
	data.Version = s.Version
	var rendered bytes.Buffer
	if err := assets.pageTemplate.Execute(&rendered, data); err != nil {
		http.Error(w, "页面模板渲染失败", http.StatusInternalServerError)
		return
	}
	body := rendered.Bytes()
	if data.Lang != data.DefaultLocale {
		messages := assets.localeMessages[data.Lang]
		localized, err := localizeRenderedHTML(body, messages)
		if err != nil {
			http.Error(w, "页面国际化渲染失败", http.StatusInternalServerError)
			return
		}
		body = localized
	}
	w.WriteHeader(data.StatusCode)
	_, _ = w.Write(body)
}

func (s Server) renderTemplateBody(name string, payload any) (template.HTML, error) {
	assets, err := s.assets()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	switch name {
	case "download":
		err = assets.downloadTmpl.Execute(&buf, payload)
	case "download_pow":
		err = assets.downloadPowTmpl.Execute(&buf, payload)
	case "blocked":
		err = assets.blockedTmpl.Execute(&buf, payload)
	case "punishment_pow":
		err = assets.punishmentPowTmpl.Execute(&buf, payload)
	case "project":
		err = assets.projectTmpl.Execute(&buf, payload)
	default:
		err = assets.pageTemplate.ExecuteTemplate(&buf, name, payload)
	}
	return template.HTML(buf.String()), err
}
