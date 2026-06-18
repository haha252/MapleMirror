package public

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

type downloadProjectView struct {
	ProjectID                string            `json:"project_id"`
	DisplayName              string            `json:"display_name"`
	Repository               string            `json:"repository"`
	Available                bool              `json:"available"`
	UnavailableReason        string            `json:"unavailable_reason"`
	IconURL                  string            `json:"icon_url"`
	ArchitectureMatchEnabled bool              `json:"architecture_match_enabled"`
	SystemMatchEnabled       bool              `json:"system_match_enabled"`
	LatestPublishedAt        string            `json:"latest_published_at"`
	DefaultVersion           string            `json:"default_version"`
	Assets                   []downloadAssetUI `json:"assets"`
}

type downloadAssetUI struct {
	AssetID           string `json:"asset_id"`
	Version           string `json:"version"`
	DownloadPath      string `json:"download_path"`
	FileName          string `json:"file_name"`
	Architecture      string `json:"architecture"`
	System            string `json:"system"`
	SizeBytes         int64  `json:"size_bytes"`
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason"`
}

func (s Server) downloadPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.downloadReadablePowPage(w, r)
		return
	}
	s.trackPageView(w, r)
	body, err := s.renderDownloadBody()
	if err != nil {
		http.Error(w, "下载页面渲染失败", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, pageData{
		Title:         "枫源镜像",
		BrowserTitle:  "枫源镜像",
		Subtitle:      mirrorDescription,
		BeforeNotices: s.currentNotices(),
		Description:   mirrorDescription,
		BodyClass:     "page-download",
		Body:          body,
		Styles:        []string{"/static/public/download.css"},
		Scripts:       []string{"/static/public/download-selectors.js", "/static/public/download.js"},
	})
}

func (s Server) catalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	views, err := s.downloadCatalog(r)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "项目列表读取失败")
		return
	}
	body, err := json.Marshal(map[string]any{"projects": views})
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "项目列表编码失败")
		return
	}
	sum := sha256.Sum256(body)
	etag := fmt.Sprintf(`"catalog-%x"`, sum[:12])
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=30, must-revalidate")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s Server) downloadCatalog(r *http.Request) ([]downloadProjectView, error) {
	projects, err := s.Store.Projects(r.Context())
	if err != nil {
		return nil, err
	}
	views := make([]downloadProjectView, 0, len(projects))
	projectAssets := s.currentProjectAssets()
	for _, project := range projects {
		assets, err := s.Store.Assets(r.Context(), project.ProjectID)
		if err != nil {
			return nil, err
		}
		views = append(views, buildDownloadProjectView(project, assets, projectAssets[project.ProjectID]))
	}
	return views, nil
}

func buildDownloadProjectView(project ProjectSummary, assets []AssetSummary, config projectAssetConfig) downloadProjectView {
	view := downloadProjectView{
		ProjectID:                project.ProjectID,
		DisplayName:              project.DisplayName,
		Repository:               project.Repository,
		Available:                project.Available,
		UnavailableReason:        project.UnavailableReason,
		IconURL:                  "/static/project-icons/" + project.ProjectID,
		ArchitectureMatchEnabled: config.ArchitectureMatchEnabled,
		SystemMatchEnabled:       config.SystemMatchEnabled,
		LatestPublishedAt:        displayDate(project.LatestPublishedAt),
		Assets:                   make([]downloadAssetUI, 0, len(assets)),
	}
	if len(assets) > 0 {
		view.DefaultVersion = assets[0].Version
	}
	for _, asset := range assets {
		view.Assets = append(view.Assets, downloadAssetUI{
			AssetID:           asset.AssetID,
			Version:           asset.Version,
			DownloadPath:      asset.DownloadPath,
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

func (s Server) renderDownloadBody() (template.HTML, error) {
	return s.renderTemplateBody("download", nil)
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
