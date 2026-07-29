package public

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"

	"mirror-server/internal/config"
)

type catalogResponse struct {
	FilterGroups      []config.FilterSelector `json:"filter_groups"`
	Projects          []downloadProjectView   `json:"projects"`
	SuggestedProjects []downloadProjectView   `json:"suggested_projects"`
}

func (s Server) catalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	payload, err := s.catalogPayload(r)
	if err != nil {
		if err == errInvalidCatalogQuery {
			writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "搜索或筛选条件无效")
		} else {
			writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "项目列表读取失败")
		}
		return
	}
	w.Header().Set("ETag", payload.ETag)
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Header.Get("If-None-Match") == payload.ETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload.Body)
}

func (s Server) catalogPayload(r *http.Request) (catalogPayload, error) {
	if s.CatalogIndex == nil {
		return s.legacyCatalogPayload(r.Context())
	}
	result, err := s.CatalogIndex.resolve(r.URL.Query())
	if err != nil {
		return catalogPayload{}, err
	}
	key := fmt.Sprintf("%d\x00%s", result.Snapshot.Generation, result.Query.CacheKey)
	load := func(ctx context.Context) (catalogPayload, error) {
		projects, err := s.catalogViewsForIDs(ctx, result.Snapshot, result.Projects)
		if err != nil {
			return catalogPayload{}, err
		}
		suggestions, err := s.catalogViewsForIDs(ctx, result.Snapshot, result.Suggestions)
		if err != nil {
			return catalogPayload{}, err
		}
		return encodeCatalogResponse(catalogResponse{
			FilterGroups: append([]config.FilterSelector(nil), result.Snapshot.Filters.Selectors...),
			Projects:     projects, SuggestedProjects: suggestions,
		})
	}
	if s.CatalogCache == nil {
		return load(r.Context())
	}
	return s.CatalogCache.getOrLoad(r.Context(), key, load)
}

func (s Server) legacyCatalogPayload(ctx context.Context) (catalogPayload, error) {
	views, err := s.downloadCatalogContext(ctx)
	if err != nil {
		return catalogPayload{}, err
	}
	return encodeCatalogResponse(catalogResponse{
		FilterGroups: []config.FilterSelector{}, Projects: views,
		SuggestedProjects: []downloadProjectView{},
	})
}

func (s Server) downloadCatalog(r *http.Request) ([]downloadProjectView, error) {
	return s.downloadCatalogContext(r.Context())
}

func (s Server) downloadCatalogContext(ctx context.Context) ([]downloadProjectView, error) {
	projects, err := s.Store.Projects(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]downloadProjectView, 0, len(projects))
	projectAssets := s.currentProjectAssets()
	for _, project := range projects {
		assets, err := s.Store.Assets(ctx, project.ProjectID)
		if err != nil {
			return nil, err
		}
		views = append(views, buildDownloadProjectView(project, assets, projectAssets[project.ProjectID]))
	}
	return views, nil
}

func (s Server) catalogViewsForIDs(ctx context.Context, snapshot *catalogSnapshot,
	ids []string) ([]downloadProjectView, error) {
	views := make([]downloadProjectView, 0, len(ids))
	if len(ids) == 0 {
		return views, nil
	}
	projects, err := s.Store.Projects(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]ProjectSummary, len(projects))
	for _, project := range projects {
		byID[project.ProjectID] = project
	}
	for _, id := range ids {
		project, ok := byID[id]
		indexed, indexedOK := snapshot.ByID[id]
		if !ok || !indexedOK {
			continue
		}
		assets, err := s.Store.Assets(ctx, id)
		if err != nil {
			return nil, err
		}
		view := buildDownloadProjectView(project, assets, indexed.Assets)
		view.Tags = cloneProjectTags(indexed.Tags)
		views = append(views, view)
	}
	return views, nil
}

func encodeCatalogResponse(response catalogResponse) (catalogPayload, error) {
	body, err := json.Marshal(response)
	if err != nil {
		return catalogPayload{}, err
	}
	sum := sha256.Sum256(body)
	return catalogPayload{Body: body, ETag: fmt.Sprintf(`"catalog-%x"`, sum[:12])}, nil
}
