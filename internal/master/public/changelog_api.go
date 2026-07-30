package public

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"mirror-server/internal/config"
)

const (
	defaultChangelogLimit = 20
	maxChangelogLimit     = 50
	changelogCursorV1     = 1
)

var errInvalidChangelogQuery = errors.New("更新日志查询条件无效")

type changelogAPIItem struct {
	Level           string `json:"level"`
	Title           string `json:"title"`
	OccurredAt      string `json:"occurred_at"`
	DescriptionHTML string `json:"description_html"`
}

type changelogAPIResponse struct {
	Items      []changelogAPIItem `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type changelogQuery struct {
	MinimumLevel string
	Search       string
	Limit        int
	Offset       int
	Generation   uint64
}

type changelogCursor struct {
	Version    int    `json:"v"`
	Generation uint64 `json:"g"`
	QueryHash  string `json:"q"`
	Offset     int    `json:"o"`
	Limit      int    `json:"n"`
}

func (s Server) changelogAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	snapshot := s.changelogSnapshot()
	query, err := parseChangelogQuery(r, snapshot.Generation)
	if err != nil {
		if errors.Is(err, errChangelogChanged) {
			writeError(w, r, http.StatusConflict, "CHANGELOG_CHANGED", "更新日志已经变化，请重新加载")
		} else {
			writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "更新日志查询条件无效")
		}
		return
	}
	response, err := queryChangelog(snapshot, query)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "更新日志查询条件无效")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeOK(w, r, http.StatusOK, "查询成功", response)
}

var errChangelogChanged = errors.New("更新日志已经变化")

func (s Server) changelogSnapshot() *changelogSnapshot {
	if s.changelog == nil {
		return &changelogSnapshot{Generation: 1}
	}
	return s.changelog.current()
}

func parseChangelogQuery(r *http.Request, generation uint64) (changelogQuery, error) {
	values := r.URL.Query()
	query := changelogQuery{
		MinimumLevel: strings.TrimSpace(values.Get("minimum_level")),
		Search:       strings.ToLower(strings.TrimSpace(values.Get("q"))),
		Limit:        defaultChangelogLimit, Generation: generation,
	}
	if query.MinimumLevel == "" {
		query.MinimumLevel = "info"
	}
	if config.ChangelogLevelRank(query.MinimumLevel) < 0 || utf8.RuneCountInString(query.Search) > 100 {
		return query, errInvalidChangelogQuery
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 || limit > maxChangelogLimit {
			return query, errInvalidChangelogQuery
		}
		query.Limit = limit
	}
	rawCursor := strings.TrimSpace(values.Get("cursor"))
	if rawCursor == "" {
		return query, nil
	}
	cursor, err := decodeChangelogCursor(rawCursor)
	if err != nil || cursor.Version != changelogCursorV1 || cursor.Offset < 0 ||
		cursor.Limit <= 0 || cursor.Limit > maxChangelogLimit ||
		cursor.QueryHash != changelogQueryHash(query.MinimumLevel, query.Search) {
		return query, errInvalidChangelogQuery
	}
	if cursor.Generation != generation {
		return query, errChangelogChanged
	}
	if values.Get("limit") == "" {
		query.Limit = cursor.Limit
	} else if query.Limit != cursor.Limit {
		return query, errInvalidChangelogQuery
	}
	query.Offset = cursor.Offset
	return query, nil
}

func queryChangelog(snapshot *changelogSnapshot, query changelogQuery) (changelogAPIResponse, error) {
	minimum := config.ChangelogLevelRank(query.MinimumLevel)
	filtered := make([]changelogEntry, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if config.ChangelogLevelRank(entry.Level) < minimum ||
			query.Search != "" && !strings.Contains(entry.SearchText, query.Search) {
			continue
		}
		filtered = append(filtered, entry)
	}
	if query.Offset > len(filtered) {
		return changelogAPIResponse{}, errInvalidChangelogQuery
	}
	end := len(filtered)
	if query.Limit < len(filtered)-query.Offset {
		end = query.Offset + query.Limit
	}
	items := make([]changelogAPIItem, 0, end-query.Offset)
	for _, entry := range filtered[query.Offset:end] {
		items = append(items, changelogAPIItem{
			Level: entry.Level, Title: entry.Title,
			OccurredAt:      entry.OccurredAt.Format(timeRFC3339),
			DescriptionHTML: entry.DescriptionHTML,
		})
	}
	response := changelogAPIResponse{Items: items}
	if end < len(filtered) {
		response.NextCursor = encodeChangelogCursor(snapshot.Generation,
			query.MinimumLevel, query.Search, end, query.Limit)
	}
	return response, nil
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

func changelogQueryHash(level, search string) string {
	sum := sha256.Sum256([]byte(level + "\x00" + search))
	return hex.EncodeToString(sum[:16])
}

func encodeChangelogCursor(generation uint64, level, search string, offset, limit int) string {
	data, _ := json.Marshal(changelogCursor{
		Version: changelogCursorV1, Generation: generation,
		QueryHash: changelogQueryHash(level, search), Offset: offset, Limit: limit,
	})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeChangelogCursor(raw string) (changelogCursor, error) {
	var cursor changelogCursor
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return cursor, err
	}
	err = json.Unmarshal(data, &cursor)
	return cursor, err
}
