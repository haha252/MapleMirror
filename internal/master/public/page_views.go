package public

import (
	"net/http"
	"sync"
	"time"

	"mirror-server/internal/requestid"
)

const pageViewCookie = "mirror_page_visitor"

var defaultPageViews = newPageViewTracker()

type pageViewTracker struct {
	mu   sync.Mutex
	day  string
	seen map[string]struct{}
}

func newPageViewTracker() *pageViewTracker {
	return &pageViewTracker{seen: map[string]struct{}{}}
}

func (t *pageViewTracker) shouldCount(day, visitorID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.day != day {
		t.day = day
		t.seen = map[string]struct{}{}
	}
	if _, ok := t.seen[visitorID]; ok {
		return false
	}
	t.seen[visitorID] = struct{}{}
	return true
}

func visitorID(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(pageViewCookie); err == nil && validVisitorID(cookie.Value) {
		return cookie.Value
	}
	id, err := requestid.New()
	if err != nil {
		id = time.Now().UTC().Format("20060102150405.000000000")
	}
	http.SetCookie(w, &http.Cookie{
		Name:     pageViewCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   60 * 60 * 24 * 180,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return id
}

func validVisitorID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}
