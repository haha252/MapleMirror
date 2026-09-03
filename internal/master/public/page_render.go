package public

import (
	"bytes"
	"html/template"
	"net/http"
)

type pageData struct {
	Title           string
	BrowserTitle    string
	Subtitle        string
	BeforeNotices   []noticeView
	AfterNotices    []noticeView
	Description     string
	CanonicalURL    string
	AlternateZHURL  string
	AlternateENURL  string
	ZHPath          string
	ENPath          string
	Lang            string
	Robots          string
	BodyClass       string
	HideHeader      bool
	Version         string
	CatalogSearch   bool
	ChangelogSearch bool
	Body            template.HTML
	Styles          []string
	Scripts         []string
	StaticNames     []string
	StaticJSON      template.JS
	StatusCode      int
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
	if data.Lang == "" {
		data.Lang = publicLocaleZH
	}
	if data.StatusCode == 0 {
		data.StatusCode = http.StatusOK
	}
	data.Version = s.Version
	w.WriteHeader(data.StatusCode)
	_ = assets.pageTemplate.Execute(w, data)
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
