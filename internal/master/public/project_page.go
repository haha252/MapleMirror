package public

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
)

const projectPageServiceDescription = "枫源镜像 是一个公益镜像服务，面向 Github Release 设计。我们致力于为所有用户提供，免费、纯净、高速且稳定的下载服务，获取到软件的最新版本。"

type projectPageBody struct {
	ProjectID         string
	DisplayName       string
	Repository        string
	Description       string
	HomepageURL       string
	IconURL           string
	LatestPublishedAt string
}

func (s Server) maybeProjectPage(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet || !singleProjectPath(r.URL.EscapedPath()) {
		return false
	}
	projectID, err := projectIDFromPath(r.URL.EscapedPath())
	if err != nil {
		return false
	}
	project, err := s.projectForPage(r, projectID)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return true
	}
	if err != nil {
		http.Error(w, "项目页面读取失败", http.StatusInternalServerError)
		return true
	}
	body, err := s.renderTemplateBody("project", project)
	if err != nil {
		http.Error(w, "项目页面渲染失败", http.StatusInternalServerError)
		return true
	}
	s.trackPageView(w, r)
	s.renderPage(w, pageData{
		Title:        project.DisplayName,
		BrowserTitle: project.DisplayName + "下载 - 枫源镜像",
		Description:  projectPageDescription(project.Description),
		BodyClass:    "page-project",
		Body:         body,
		Styles:       []string{"/static/public/project.css", "/static/public/project-responsive.css"},
		Scripts:      []string{"/static/public/download-selectors.js", "/static/public/project.js"},
	})
	return true
}

func projectPageDescription(description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return projectPageServiceDescription
	}
	return description + " " + projectPageServiceDescription
}

func (s Server) projectForPage(r *http.Request, projectID string) (projectPageBody, error) {
	projects, err := s.downloadCatalog(r)
	if err != nil {
		return projectPageBody{}, err
	}
	for _, project := range projects {
		if project.ProjectID != projectID {
			continue
		}
		return projectPageBody{
			ProjectID:         project.ProjectID,
			DisplayName:       project.DisplayName,
			Repository:        project.Repository,
			Description:       strings.TrimSpace(project.Description),
			HomepageURL:       strings.TrimSpace(project.HomepageURL),
			IconURL:           project.IconURL,
			LatestPublishedAt: project.LatestPublishedAt,
		}, nil
	}
	return projectPageBody{}, sql.ErrNoRows
}

func singleProjectPath(path string) bool {
	trimmed := strings.Trim(path, "/")
	return trimmed != "" && !strings.Contains(trimmed, "/")
}

func projectIDFromPath(path string) (string, error) {
	decoded, err := url.PathUnescape(strings.Trim(path, "/"))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(decoded) == "" || strings.Contains(decoded, "/") {
		return "", sql.ErrNoRows
	}
	return decoded, nil
}
