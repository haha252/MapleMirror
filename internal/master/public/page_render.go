package public

import (
	"bytes"
	"html/template"
	"net/http"
)

type pageData struct {
	Title         string
	BrowserTitle  string
	Subtitle      string
	BeforeNotices []noticeView
	AfterNotices  []noticeView
	Description   string
	BodyClass     string
	HideHeader    bool
	Body          template.HTML
	Styles        []string
	Scripts       []string
	StaticJSON    template.JS
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
	data.StaticJSON = assets.staticJSON
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
	default:
		err = assets.pageTemplate.ExecuteTemplate(&buf, name, payload)
	}
	return template.HTML(buf.String()), err
}
