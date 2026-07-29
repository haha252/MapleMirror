package public

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultCatalogBatchRows = 4
	maxCatalogColumns       = 3
	catalogCursorVersion    = 1
	catalogSectionProjects  = "projects"
	catalogSectionSuggested = "suggested_projects"
)

var errCatalogChanged = errors.New("目录已经更新")

type catalogPageRequest struct {
	Enabled  bool
	Section  string
	Offset   int
	PageSize int
}

type catalogCursor struct {
	Version    int    `json:"v"`
	Generation uint64 `json:"g"`
	QueryHash  string `json:"q"`
	Section    string `json:"s"`
	Offset     int    `json:"o"`
	PageSize   int    `json:"n"`
}

func (s Server) parseCatalogPage(values url.Values, result catalogQueryResult) (catalogPageRequest, error) {
	rawSize := strings.TrimSpace(values.Get("page_size"))
	rawCursor := strings.TrimSpace(values.Get("cursor"))
	if rawSize == "" && rawCursor == "" {
		return catalogPageRequest{}, nil
	}
	maximum := s.catalogMaxPageSize()
	if rawCursor == "" {
		size, err := strconv.Atoi(rawSize)
		if err != nil || size <= 0 || size > maximum {
			return catalogPageRequest{}, errInvalidCatalogQuery
		}
		return catalogPageRequest{Enabled: true, PageSize: size}, nil
	}
	cursor, err := decodeCatalogCursor(rawCursor)
	if err != nil || cursor.Version != catalogCursorVersion ||
		cursor.Offset < 0 || cursor.PageSize <= 0 || cursor.PageSize > maximum ||
		(cursor.Section != catalogSectionProjects && cursor.Section != catalogSectionSuggested) ||
		cursor.QueryHash != catalogQueryHash(result.Query) {
		return catalogPageRequest{}, errInvalidCatalogQuery
	}
	if rawSize != "" {
		size, sizeErr := strconv.Atoi(rawSize)
		if sizeErr != nil || size <= 0 || size > maximum {
			return catalogPageRequest{}, errInvalidCatalogQuery
		}
		cursor.PageSize = size
	}
	if cursor.Generation != result.Snapshot.Generation {
		return catalogPageRequest{}, errCatalogChanged
	}
	return catalogPageRequest{
		Enabled: true, Section: cursor.Section,
		Offset: cursor.Offset, PageSize: cursor.PageSize,
	}, nil
}

func (s Server) catalogMaxPageSize() int {
	rows := s.catalogBatchRows()
	maximum := int(^uint(0) >> 1)
	if rows > maximum/maxCatalogColumns {
		return maximum
	}
	return rows * maxCatalogColumns
}

func decodeCatalogCursor(value string) (catalogCursor, error) {
	var cursor catalogCursor
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor, err
	}
	if err := json.Unmarshal(data, &cursor); err != nil {
		return cursor, err
	}
	return cursor, nil
}

func encodeCatalogCursor(generation uint64, query catalogQuery, section string,
	offset, pageSize int) string {
	data, _ := json.Marshal(catalogCursor{
		Version: catalogCursorVersion, Generation: generation,
		QueryHash: catalogQueryHash(query), Section: section,
		Offset: offset, PageSize: pageSize,
	})
	return base64.RawURLEncoding.EncodeToString(data)
}

func catalogQueryHash(query catalogQuery) string {
	sum := sha256.Sum256([]byte(query.CacheKey))
	return hex.EncodeToString(sum[:16])
}

func (p catalogPageRequest) cacheKey() string {
	if !p.Enabled {
		return "full"
	}
	return fmt.Sprintf("page:%s:%d:%d", p.Section, p.Offset, p.PageSize)
}

func catalogPageIDs(ids []string, page catalogPageRequest) ([]string, int, error) {
	if page.Offset < 0 || page.Offset >= len(ids) {
		return nil, 0, errInvalidCatalogQuery
	}
	end := len(ids)
	if page.PageSize < len(ids)-page.Offset {
		end = page.Offset + page.PageSize
	}
	return ids[page.Offset:end], end, nil
}
