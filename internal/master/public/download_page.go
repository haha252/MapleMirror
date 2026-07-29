package public

import (
	"html/template"
	"net/http"
	"strings"

	"mirror-server/internal/config"
)

type downloadProjectView struct {
	ProjectID                   string              `json:"project_id"`
	DisplayName                 string              `json:"display_name"`
	Repository                  string              `json:"repository"`
	Description                 string              `json:"description"`
	HomepageURL                 string              `json:"homepage_url"`
	Available                   bool                `json:"available"`
	UnavailableReason           string              `json:"unavailable_reason"`
	IconURL                     string              `json:"icon_url"`
	ArchitectureMatchEnabled    bool                `json:"architecture_match_enabled"`
	SystemMatchEnabled          bool                `json:"system_match_enabled"`
	ArchitectureSelectorEnabled bool                `json:"architecture_selector_enabled"`
	SystemSelectorEnabled       bool                `json:"system_selector_enabled"`
	DefaultSelectionMode        string              `json:"default_selection_mode"`
	LatestPublishedAt           string              `json:"latest_published_at"`
	DefaultVersion              string              `json:"default_version"`
	Tags                        map[string][]string `json:"tags,omitempty"`
	Assets                      []downloadAssetUI   `json:"assets"`
}

type downloadAssetUI struct {
	AssetID           string `json:"asset_id"`
	Version           string `json:"version"`
	DownloadPath      string `json:"download_path"`
	FileName          string `json:"file_name"`
	Architecture      string `json:"architecture"`
	System            string `json:"system"`
	Variant           string `json:"variant,omitempty"`
	DisplayLabel      string `json:"display_label,omitempty"`
	Priority          int    `json:"priority,omitempty"`
	SizeBytes         int64  `json:"size_bytes"`
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason"`
}

type downloadPageData struct {
	Notices []noticeView
}

func (s Server) downloadPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		if s.maybeProjectPage(w, r) {
			return
		}
		s.downloadReadablePowPage(w, r)
		return
	}
	s.trackPageView(w, r)
	notices := s.currentNotices()
	body, err := s.renderDownloadBody(notices)
	if err != nil {
		http.Error(w, "下载页面渲染失败", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, pageData{
		Title:         "枫源镜像",
		BrowserTitle:  "枫源镜像",
		Subtitle:      mirrorDescription,
		Description:   mirrorDescription,
		BodyClass:     "page-download",
		CatalogSearch: true,
		Body:          body,
		Styles: []string{"/static/public/download.css",
			"/static/public/download-filters.css"},
		Scripts: []string{"/static/public/download-selectors.js",
			"/static/public/download-file-browser.js", "/static/public/download-masonry.js",
			"/static/public/download-card.js", "/static/public/download-filters.js",
			"/static/public/download.js"},
	})
}

func buildDownloadProjectView(project ProjectSummary, assets []AssetSummary, assetConfig projectAssetConfig) downloadProjectView {
	view := downloadProjectView{
		ProjectID:                   project.ProjectID,
		DisplayName:                 project.DisplayName,
		Repository:                  project.Repository,
		Description:                 project.Description,
		HomepageURL:                 project.HomepageURL,
		Available:                   project.Available,
		UnavailableReason:           project.UnavailableReason,
		IconURL:                     "/static/project-icons/" + project.ProjectID,
		ArchitectureMatchEnabled:    assetConfig.ArchitectureSelectorEnabled,
		SystemMatchEnabled:          assetConfig.SystemSelectorEnabled,
		ArchitectureSelectorEnabled: assetConfig.ArchitectureSelectorEnabled,
		SystemSelectorEnabled:       assetConfig.SystemSelectorEnabled,
		DefaultSelectionMode:        normalizedDefaultSelectionMode(assetConfig.DefaultSelectionMode),
		LatestPublishedAt:           displayDate(project.LatestPublishedAt),
		Assets:                      make([]downloadAssetUI, 0, len(assets)),
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
			Variant:           asset.Variant,
			DisplayLabel:      asset.DisplayLabel,
			Priority:          asset.Priority,
			SizeBytes:         asset.SizeBytes,
			Available:         asset.Available,
			UnavailableReason: asset.UnavailableReason,
		})
	}
	return view
}

func normalizedDefaultSelectionMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), config.ProjectSelectionModeFile) {
		return config.ProjectSelectionModeFile
	}
	return config.ProjectSelectionModeSelectors
}

func (s Server) renderDownloadBody(notices []noticeView) (template.HTML, error) {
	return s.renderTemplateBody("download", downloadPageData{Notices: notices})
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
