package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

type consoleHandler struct {
	out      io.Writer
	level    slog.Level
	location *time.Location
	attrs    []slog.Attr
	mu       *sync.Mutex
}

func newConsoleHandler(out io.Writer, level slog.Level, location *time.Location) slog.Handler {
	return &consoleHandler{out: out, level: level, location: location, mu: &sync.Mutex{}}
}

func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *consoleHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := make([]slog.Attr, 0, len(h.attrs)+record.NumAttrs())
	attrs = append(attrs, h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})

	component := ""
	fields := make([]string, 0, len(attrs))
	for _, attr := range attrs {
		attr.Value = attr.Value.Resolve()
		if attr.Key == "" {
			continue
		}
		if attr.Key == "component" {
			component = attr.Value.String()
			continue
		}
		if !showConsoleField(record.Message, attr.Key) {
			continue
		}
		fields = append(fields, fieldLabelForMessage(record.Message, attr.Key)+"="+formatConsoleValueForKey(attr.Key, attr.Value))
	}

	timestamp := record.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	if h.location != nil {
		timestamp = timestamp.In(h.location)
	}

	var line strings.Builder
	line.WriteString(timestamp.Format("15:04:05"))
	line.WriteByte(' ')
	line.WriteString(levelLabel(record.Level))
	if component != "" {
		line.WriteByte(' ')
		line.WriteString(component)
	}
	line.WriteByte(' ')
	line.WriteString(record.Message)
	if len(fields) > 0 {
		line.WriteString("，")
		line.WriteString(strings.Join(fields, "，"))
	}
	line.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, line.String())
	return err
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *consoleHandler) WithGroup(_ string) slog.Handler {
	return h
}

func levelLabel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "错误"
	case level >= slog.LevelWarn:
		return "警告"
	case level <= slog.LevelDebug:
		return "调试"
	default:
		return "信息"
	}
}

func fieldLabel(key string) string {
	if label, ok := consoleFieldLabels[key]; ok {
		return label
	}
	return strings.ReplaceAll(key, "_", "-")
}

func fieldLabelForMessage(message, key string) string {
	if message == "下载令牌已签发" && key == "node_name" {
		return "节点"
	}
	return fieldLabel(key)
}

func showConsoleField(message, key string) bool {
	if message != "下载令牌已签发" {
		return true
	}
	switch key {
	case "client_ip", "client_source", "node_name", "project_id", "system",
		"architecture", "pow_difficulty", "pow_algorithm", "pow_protocol_version",
		"pow_iterations", "pow_multiplier", "modulus_id", "challenge_age_ms", "request_remaining_tokens",
		"traffic_remaining_bytes":
		return true
	default:
		return false
	}
}

func formatConsoleValueForKey(key string, value slog.Value) string {
	if key == "traffic_remaining_bytes" {
		return formatTrafficRemaining(value)
	}
	return formatConsoleValue(value)
}

func formatConsoleValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		return value.String()
	case slog.KindBool:
		if value.Bool() {
			return "是"
		}
		return "否"
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().Format(time.RFC3339)
	case slog.KindAny:
		return formatAny(value.Any())
	default:
		return value.String()
	}
}

func formatTrafficRemaining(value slog.Value) string {
	value = value.Resolve()
	switch value.Kind() {
	case slog.KindInt64:
		return formatBytesAsGB(value.Int64())
	case slog.KindUint64:
		return formatBytesAsGB(int64(value.Uint64()))
	case slog.KindAny:
		switch v := value.Any().(type) {
		case map[string]int64:
			return formatStringInt64MapWith(v, formatBytesAsGB)
		case int:
			return formatBytesAsGB(int64(v))
		case int64:
			return formatBytesAsGB(v)
		default:
			return formatAny(v)
		}
	default:
		return formatConsoleValue(value)
	}
}

func formatBytesAsGB(bytes int64) string {
	const gb = 1024 * 1024 * 1024
	if bytes%gb == 0 {
		return fmt.Sprintf("%dGB", bytes/gb)
	}
	return fmt.Sprintf("%.2fGB", float64(bytes)/gb)
}

func formatAny(value any) string {
	switch v := value.(type) {
	case nil:
		return "<nil>"
	case map[string]int64:
		return formatStringInt64MapWith(v, func(value int64) string {
			return fmt.Sprintf("%d", value)
		})
	case map[string]any:
		return formatStringAnyMap(v)
	default:
		return fmt.Sprint(v)
	}
}

func formatStringInt64MapWith(values map[string]int64, format func(int64) string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, format(values[key])))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func formatStringAnyMap(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, values[key]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func selectedLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
