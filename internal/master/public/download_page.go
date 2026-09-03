package public

import (
	"html/template"
	"net/http"
	"strings"
	"time"

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
	Notices                      []noticeView
	CatalogBatchRows             int
	CatalogPrefetchRemainingRows int
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
	body, err := s.renderDownloadBody(notices, s.catalogBatchRows(),
		s.catalogPrefetchRemainingRows())
	if err != nil {
		http.Error(w, "下载页面渲染失败", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, s.localizeIndexablePage(r, "/", pageData{
		Title:         "枫源镜像",
		BrowserTitle:  "枫源镜像 - GitHub Release 软件版本与文件下载服务",
		Subtitle:      "面向 GitHub Release 的公益镜像服务，提供稳定、快速的软件版本与文件下载。",
		Description:   "枫源镜像是面向 GitHub Release 的公益镜像下载服务，提供免费、稳定、快速的软件版本与文件下载，支持项目搜索、版本筛选、镜像节点状态查看、网页验证下载和公共 API 接入，适用于网页用户、脚本工具与自动更新器。",
		BodyClass:     "page-download",
		CatalogSearch: true,
		Body:          body,
		Styles: []string{"/static/public/download.css",
			"/static/public/download-filters.css",
			"/static/public/download-filters-mobile.css"},
		Scripts: []string{"/static/public/download-selectors.js",
			"/static/public/download-file-browser.js", "/static/public/download-masonry.js",
			"/static/public/download-card.js", "/static/public/download-filters.js",
			"/static/public/download-lazy.js", "/static/public/download.js"},
	}))
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
		LatestPublishedAt:           displayDateTime(project.LatestPublishedAt),
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

func (s Server) renderDownloadBody(notices []noticeView, catalogBatchRows,
	catalogPrefetchRemainingRows int) (template.HTML, error) {
	return s.renderTemplateBody("download", downloadPageData{
		Notices: notices, CatalogBatchRows: catalogBatchRows,
		CatalogPrefetchRemainingRows: catalogPrefetchRemainingRows,
	})
}

func (s Server) catalogBatchRows() int {
	if s.CatalogBatchRows > 0 {
		return s.CatalogBatchRows
	}
	return defaultCatalogBatchRows
}

func (s Server) catalogPrefetchRemainingRows() int {
	if s.CatalogPrefetchRemainingRows != nil &&
		*s.CatalogPrefetchRemainingRows >= 0 &&
		*s.CatalogPrefetchRemainingRows < s.catalogBatchRows() {
		return *s.CatalogPrefetchRemainingRows
	}
	if s.catalogBatchRows() == 1 {
		return 0
	}
	return 1
}

func displayDateTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := parseTime(value); err == nil {
		return parsed.Format(time.RFC3339Nano)
	}
	return value
}
