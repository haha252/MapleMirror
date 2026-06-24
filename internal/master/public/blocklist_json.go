package public

import (
	"net/http"
	"time"
)

type blocklistJSONEntry struct {
	Entry              string `json:"entry"`
	Reason             string `json:"reason"`
	Comment            string `json:"comment,omitempty"`
	AttemptsAfterBlock int64  `json:"attempts_after_block,omitempty"`
	BlockedAt          string `json:"blocked_at,omitempty"`
}

func (s Server) blocklistJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	entries, expiresAt, err := s.cachedBlocklistSnapshot(r.Context(), time.Now().UTC())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "封禁列表读取失败")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
	writeOK(w, r, http.StatusOK, "查询成功", map[string]any{
		"cache_expires_at": expiresAt.Format(time.RFC3339Nano),
		"blocks":           blocklistJSONEntries(entries),
	})
}

func blocklistJSONEntries(entries []blocklistExportEntry) []blocklistJSONEntry {
	out := make([]blocklistJSONEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, blocklistJSONEntry{
			Entry:              displayBlockPrefix(entry.Prefix),
			Reason:             entry.Reason,
			Comment:            entry.Note,
			AttemptsAfterBlock: entry.Attempts,
			BlockedAt:          entry.Blocked,
		})
	}
	return out
}
