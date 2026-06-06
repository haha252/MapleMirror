package adminui

import (
	"strings"
	"time"
)

const adminTimeLayout = "2006/01/02 15:04"

func loadTimeLocation(name string) (*time.Location, error) {
	if strings.TrimSpace(name) == "" {
		return time.Local, nil
	}
	return time.LoadLocation(name)
}

func (s *Server) location() *time.Location {
	if s != nil && s.timeLocation != nil {
		return s.timeLocation
	}
	return time.Local
}

func (s *Server) displayTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.In(s.location()).Format(adminTimeLayout)
		}
	}
	return value
}

func (s *Server) displayTimeMap(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = s.displayTimeValue(key, value)
	}
	return out
}

func (s *Server) displayTimeValue(key string, value any) any {
	if value == nil || !isTimeField(key) {
		return value
	}
	if text, ok := value.(string); ok {
		return s.displayTime(text)
	}
	if t, ok := value.(time.Time); ok {
		return t.In(s.location()).Format(adminTimeLayout)
	}
	return value
}

func isTimeField(key string) bool {
	return strings.HasSuffix(key, "_at") || key == "expires_at" ||
		key == "started_at" || key == "completed_at" || key == "updated_at"
}
