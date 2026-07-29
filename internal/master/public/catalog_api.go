package public

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"mirror-server/internal/config"
)

type catalogResponse struct {
	FilterGroups        []config.FilterSelector `json:"filter_groups"`
	Projects            []downloadProjectView   `json:"projects"`
	SuggestedProjects   []downloadProjectView   `json:"suggested_projects"`
	NextProjectsCursor  string                  `json:"next_projects_cursor,omitempty"`
	NextSuggestedCursor string                  `json:"next_suggested_projects_cursor,omitempty"`
}

func (s Server) catalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	payload, err := s.catalogPayload(r)
	if err != nil {
		if errors.Is(err, errCatalogChanged) {
			writeError(w, r, http.StatusConflict, "CATALOG_CHANGED", "项目目录已经更新，请重新加载")
		} else if errors.Is(err, errInvalidCatalogQuery) {
			writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "搜索、筛选或分页条件无效")
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
	page, err := s.parseCatalogPage(r.URL.Query(), result)
	if err != nil {
		return catalogPayload{}, err
	}
	key := fmt.Sprintf("%d\x00%s\x00%s", result.Snapshot.Generation,
		result.Query.CacheKey, page.cacheKey())
	load := func(ctx context.Context) (catalogPayload, error) {
		projectIDs, suggestionIDs := result.Projects, result.Suggestions
		response := catalogResponse{
			FilterGroups: append([]config.FilterSelector(nil), result.Snapshot.Filters.Selectors...),
		}
		if page.Enabled {
			projectIDs, suggestionIDs, err = pagedCatalogIDs(result, page, &response)
			if err != nil {
				return catalogPayload{}, err
			}
		}
		projects, err := s.catalogViewsForIDs(ctx, result.Snapshot, projectIDs)
		if err != nil {
			return catalogPayload{}, err
		}
		suggestions, err := s.catalogViewsForIDs(ctx, result.Snapshot, suggestionIDs)
		if err != nil {
			return catalogPayload{}, err
		}
		response.Projects, response.SuggestedProjects = projects, suggestions
		return encodeCatalogResponse(response)
	}
	if s.CatalogCache == nil {
		return load(r.Context())
	}
	return s.CatalogCache.getOrLoad(r.Context(), key, load)
}

func pagedCatalogIDs(result catalogQueryResult, page catalogPageRequest,
	response *catalogResponse) ([]string, []string, error) {
	if page.Section == catalogSectionProjects {
		projects, end, err := catalogPageIDs(result.Projects, page)
		if err != nil {
			return nil, nil, err
		}
		if end < len(result.Projects) {
			response.NextProjectsCursor = encodeCatalogCursor(result.Snapshot.Generation,
				result.Query, catalogSectionProjects, end, page.PageSize)
		}
		return projects, nil, nil
	}
	if page.Section == catalogSectionSuggested {
		suggestions, end, err := catalogPageIDs(result.Suggestions, page)
		if err != nil {
			return nil, nil, err
		}
		if end < len(result.Suggestions) {
			response.NextSuggestedCursor = encodeCatalogCursor(result.Snapshot.Generation,
				result.Query, catalogSectionSuggested, end, page.PageSize)
		}
		return nil, suggestions, nil
	}
	projects, projectEnd := firstCatalogPage(result.Projects, page.PageSize)
	suggestions, suggestionEnd := firstCatalogPage(result.Suggestions, page.PageSize)
	if projectEnd < len(result.Projects) {
		response.NextProjectsCursor = encodeCatalogCursor(result.Snapshot.Generation,
			result.Query, catalogSectionProjects, projectEnd, page.PageSize)
	}
	if suggestionEnd < len(result.Suggestions) {
		response.NextSuggestedCursor = encodeCatalogCursor(result.Snapshot.Generation,
			result.Query, catalogSectionSuggested, suggestionEnd, page.PageSize)
	}
	return projects, suggestions, nil
}

func firstCatalogPage(ids []string, pageSize int) ([]string, int) {
	end := min(pageSize, len(ids))
	return ids[:end], end
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
