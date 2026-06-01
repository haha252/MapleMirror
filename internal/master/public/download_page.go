package public

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
)

type downloadProjectView struct {
	ProjectID          string            `json:"project_id"`
	DisplayName        string            `json:"display_name"`
	Repository         string            `json:"repository"`
	Available          bool              `json:"available"`
	UnavailableReason  string            `json:"unavailable_reason"`
	IconURL            string            `json:"icon_url"`
	SystemMatchEnabled bool              `json:"system_match_enabled"`
	LatestPublishedAt  string            `json:"latest_published_at"`
	Assets             []downloadAssetUI `json:"assets"`
}

type downloadAssetUI struct {
	AssetID           string `json:"asset_id"`
	Version           string `json:"version"`
	FileName          string `json:"file_name"`
	Architecture      string `json:"architecture"`
	System            string `json:"system"`
	SizeBytes         int64  `json:"size_bytes"`
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason"`
}

func (s Server) downloadPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.trackPageView(w, r)
	projects, err := s.Store.Projects(r.Context())
	if err != nil {
		http.Error(w, "项目列表读取失败", http.StatusInternalServerError)
		return
	}
	views := make([]downloadProjectView, 0, len(projects))
	projectAssets := s.currentProjectAssets()
	for _, project := range projects {
		assets, err := s.Store.Assets(r.Context(), project.ProjectID)
		if err != nil {
			http.Error(w, "资产列表读取失败", http.StatusInternalServerError)
			return
		}
		views = append(views, buildDownloadProjectView(project, assets, projectAssets[project.ProjectID]))
	}
	body, err := s.renderDownloadBody(views)
	if err != nil {
		http.Error(w, "下载页面渲染失败", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, pageData{
		Title:     "下载",
		Subtitle:  "选择版本与架构，开始下载！",
		BodyClass: "page-download",
		Body:      body,
		Styles:    []string{"/static/public/download.css"},
		Scripts:   []string{"/static/public/download-selectors.js", "/static/public/pow-loader.js", "/static/public/download.js"},
	})
}

func buildDownloadProjectView(project ProjectSummary, assets []AssetSummary, config projectAssetConfig) downloadProjectView {
	view := downloadProjectView{
		ProjectID:          project.ProjectID,
		DisplayName:        project.DisplayName,
		Repository:         project.Repository,
		Available:          project.Available,
		UnavailableReason:  project.UnavailableReason,
		IconURL:            "/static/project-icons/" + project.ProjectID,
		SystemMatchEnabled: config.SystemMatchEnabled,
		LatestPublishedAt:  displayDate(project.LatestPublishedAt),
		Assets:             make([]downloadAssetUI, 0, len(assets)),
	}
	for _, asset := range assets {
		view.Assets = append(view.Assets, downloadAssetUI{
			AssetID:           asset.AssetID,
			Version:           asset.Version,
			FileName:          asset.FileName,
			Architecture:      asset.Architecture,
			System:            asset.System,
			SizeBytes:         asset.SizeBytes,
			Available:         asset.Available,
			UnavailableReason: asset.UnavailableReason,
		})
	}
	return view
}

func (s Server) renderDownloadBody(projects []downloadProjectView) (template.HTML, error) {
	body := struct {
		ProjectsJSON template.JS
	}{ProjectsJSON: template.JS("[]")}
	data, err := json.Marshal(projects)
	if err != nil {
		return "", err
	}
	body.ProjectsJSON = template.JS(string(data))
	return s.renderTemplateBody("download", body)
}

func displayDate(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	if parsed, err := parseTime(value); err == nil {
		return parsed.Format("2006/1/2")
	}
	return value
}
