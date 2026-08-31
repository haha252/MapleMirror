package public

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mirror-server/internal/assetpath"
)

type downloadPowAssetUI struct {
	AssetID           string `json:"asset_id"`
	ProjectName       string `json:"project_name"`
	Version           string `json:"version"`
	DownloadPath      string `json:"download_path"`
	SuccessPath       string `json:"success_path"`
	RetryPath         string `json:"retry_path"`
	Architecture      string `json:"architecture"`
	System            string `json:"system"`
	SizeBytes         int64  `json:"size_bytes"`
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason"`
}

type downloadPowVerificationUI struct {
	Token   string
	AssetID string
	Buttons []webVerificationButton
}

const downloadSuccessPrefix = "/download/success/"

func (s Server) downloadPowPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	if s.rejectBlockedDownload(w, r, "", "download_page") {
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
	s.renderDownloadPowPage(w, r, asset)
}

func (s Server) downloadSuccessPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	value := strings.TrimPrefix(r.URL.EscapedPath(), downloadSuccessPrefix)
	if value == "" || value == r.URL.EscapedPath() {
		http.NotFound(w, r)
		return
	}
	if _, err := assetpath.ParsePublicPath("/" + value); err != nil {
		http.NotFound(w, r)
		return
	}
	asset, err := s.Store.DownloadAssetByPath(r.Context(), "/"+value)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.renderDownloadPowPageState(w, r, asset, true)
}

func (s Server) downloadReadablePowPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	if s.rejectBlockedDownload(w, r, "", "download_page") {
		return
	}
	if _, err := assetpath.ParsePublicPath(r.URL.EscapedPath()); err != nil {
		http.NotFound(w, r)
		return
	}
	asset, err := s.Store.DownloadAssetByPath(r.Context(), r.URL.EscapedPath())
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.renderDownloadPowPage(w, r, asset)
}

func (s Server) renderDownloadPowPage(w http.ResponseWriter, r *http.Request, asset DownloadAssetSummary) {
	s.renderDownloadPowPageState(w, r, asset, false)
}

func (s Server) renderDownloadPowPageState(w http.ResponseWriter, r *http.Request,
	asset DownloadAssetSummary, success bool) {
	w.Header().Set("Cache-Control", "private, no-store")
	s.trackPageView(w, r)
	fromHome := downloadPowFromHome(r)
	body, err := s.renderDownloadPowBody(r, asset, fromHome, success)
	if err != nil {
		http.Error(w, "下载验证页面渲染失败", http.StatusInternalServerError)
		return
	}
	title, browserTitle, bodyClass := "下载验证", "下载验证 - 枫源镜像", "page-download-pow"
	if success {
		title, browserTitle = "下载已开始", "下载已开始 - 枫源镜像"
		bodyClass = "page-download-success page-download-pow"
	}
	scripts := []string{"/static/public/vdf-fallback.js", "/static/public/download-pow.js"}
	staticNames := []string{"vdf-worker.js"}
	if success {
		scripts = nil
		staticNames = []string{}
	}
	s.renderPage(w, pageData{
		Title:        title,
		BrowserTitle: browserTitle,
		Description:  "枫源镜像下载验证页",
		Robots:       noIndexRobots,
		BodyClass:    bodyClass,
		HideHeader:   true,
		AfterNotices: s.currentNotices(),
		Body:         body,
		Styles: []string{
			"/static/public/download-pow.css",
			"/static/public/download-verification.css",
			"/static/public/download-pow-mascot.css",
		},
		Scripts:     scripts,
		StaticNames: staticNames,
	})
}

func (s Server) renderDownloadPowBody(r *http.Request, asset DownloadAssetSummary,
	fromHome, success bool) (template.HTML, error) {
	successPath, err := downloadSuccessPath(asset.DownloadPath)
	if err != nil {
		return "", err
	}
	var verification *downloadPowVerificationUI
	if !success {
		verification = s.downloadPowVerification(r, asset.AssetID)
	}
	body := struct {
		AssetJSON    template.JS
		FromHome     bool
		Verification *downloadPowVerificationUI
		Success      bool
		RetryPath    string
	}{AssetJSON: template.JS("{}"), FromHome: fromHome, Success: success,
		RetryPath:    retryDownloadPath(asset.DownloadPath, fromHome),
		Verification: verification}
	data, err := json.Marshal(downloadPowAssetUI{
		AssetID:           asset.AssetID,
		ProjectName:       asset.ProjectName,
		Version:           asset.Version,
		DownloadPath:      asset.DownloadPath,
		SuccessPath:       successPath,
		RetryPath:         retryDownloadPath(asset.DownloadPath, fromHome),
		Architecture:      asset.Architecture,
		System:            asset.System,
		SizeBytes:         asset.SizeBytes,
		Available:         asset.Available,
		UnavailableReason: asset.UnavailableReason,
	})
	if err != nil {
		return "", err
	}
	body.AssetJSON = template.JS(string(data))
	return s.renderTemplateBody("download_pow", body)
}

func downloadSuccessPath(downloadPath string) (string, error) {
	if strings.TrimSpace(downloadPath) == "" {
		return "", nil
	}
	parts, err := assetpath.ParsePublicPath(downloadPath)
	if err != nil {
		return "", err
	}
	canonical := assetpath.PublicPath(parts.ProjectID, parts.Version, parts.FileName)
	return downloadSuccessPrefix + strings.TrimPrefix(canonical, "/"), nil
}

func retryDownloadPath(downloadPath string, fromHome bool) string {
	if fromHome {
		return downloadPath + "?from=home"
	}
	return downloadPath
}

func (s Server) downloadPowVerification(r *http.Request, assetID string) *downloadPowVerificationUI {
	if s.webVerificationMode() == "off" || s.WebVerifications == nil {
		return nil
	}
	prefix := s.clientPrefix(r)
	if prefix == "" || prefix == "unknown" {
		return nil
	}
	token, err := s.WebVerifications.issue(assetID, prefix, time.Now().UTC())
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn(r.Context(), "网页验证临时令牌生成失败",
				slog.String("request_id", requestID(r)),
				slog.String("asset_id", assetID),
				slog.String("error", err.Error()))
		}
		return nil
	}
	return &downloadPowVerificationUI{
		Token: token, AssetID: assetID, Buttons: newWebVerificationButtons(),
	}
}

func downloadPowFromHome(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("from")), "home") {
		return true
	}
	return refererIsSiteHome(r)
}

func refererIsSiteHome(r *http.Request) bool {
	raw := strings.TrimSpace(r.Referer())
	if raw == "" {
		return false
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if ref.IsAbs() {
		if r.Host == "" || !strings.EqualFold(ref.Host, r.Host) {
			return false
		}
	} else if ref.Host != "" {
		return false
	}
	return ref.EscapedPath() == "/" || ref.EscapedPath() == ""
}
