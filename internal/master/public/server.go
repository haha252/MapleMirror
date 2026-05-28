package public

import (
	"database/sql"
	"net/http"
	"strings"
)

type Server struct {
	Store Store
}

func New(db *sql.DB) Server {
	return Server{Store: Store{DB: db}}
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.downloadPage)
	mux.HandleFunc("/stats", s.statsPage)
	mux.HandleFunc("/about", s.aboutPage)
	mux.HandleFunc("/nodes", s.nodesPage)
	mux.HandleFunc("/api/public/v1/projects", s.projects)
	mux.HandleFunc("/api/public/v1/projects/", s.projectAssets)
	return mux
}

func (s Server) projects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	projects, err := s.Store.Projects(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "项目查询失败")
		return
	}
	writeOK(w, r, http.StatusOK, "查询成功", map[string]any{"projects": projects})
}

func (s Server) projectAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	prefix := "/api/public/v1/projects/"
	path := strings.TrimPrefix(r.URL.Path, prefix)
	projectID, ok := strings.CutSuffix(path, "/assets")
	if !ok || projectID == "" || strings.Contains(projectID, "/") {
		writeError(w, r, http.StatusNotFound, "ASSET_NOT_FOUND", "项目资产不存在")
		return
	}
	assets, err := s.Store.Assets(r.Context(), projectID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "资产查询失败")
		return
	}
	writeOK(w, r, http.StatusOK, "查询成功", map[string]any{"assets": assets})
}
