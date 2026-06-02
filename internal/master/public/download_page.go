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
	DefaultVersion     string            `json:"default_version"`
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
		Title:        "枫源镜像",
		BrowserTitle: "枫源镜像",
		Subtitle:     mirrorDescription,
		Notice:       "本站目前处于测试状态，会出现不稳定，不可用的情况。预计将在六月中旬进入完全稳定的生产状态。",
		Description:  mirrorDescription,
		BodyClass:    "page-download",
		Body:         body,
		Styles:       []string{"/static/public/download.css"},
		Scripts:      []string{"/static/public/download-selectors.js", "/static/public/download.js"},
	})
}

func (s Server) downloadPowPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	assetID := strings.TrimPrefix(r.URL.Path, "/download/")
	if assetID == "" || strings.Contains(assetID, "/") {
		http.NotFound(w, r)
		return
	}
	asset, err := s.Store.DownloadAsset(r.Context(), assetID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body, err := s.renderDownloadPowBody(asset)
	if err != nil {
		http.Error(w, "下载验证页面渲染失败", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, pageData{
		Title:        "下载验证",
		BrowserTitle: asset.FileName + " - 下载验证",
		Subtitle:     "完成浏览器验证后将自动开始下载。",
		Description:  "枫源镜像下载验证页",
		BodyClass:    "page-download-pow",
		Body:         body,
		Styles:       []string{"/static/public/download.css"},
		Scripts:      []string{"/static/public/pow-loader.js", "/static/public/download-pow.js"},
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
	if len(assets) > 0 {
		view.DefaultVersion = assets[0].Version
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

func (s Server) renderDownloadPowBody(asset DownloadAssetSummary) (template.HTML, error) {
	body := struct {
		AssetJSON template.JS
	}{AssetJSON: template.JS("{}")}
	data, err := json.Marshal(asset)
	if err != nil {
		return "", err
	}
	body.AssetJSON = template.JS(string(data))
	return s.renderTemplateBody("download_pow", body)
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
