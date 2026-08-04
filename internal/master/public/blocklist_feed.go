package public

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func parseBlocklistFeed(reader io.Reader, feedURL string) ([]blocklistEntry, error) {
	var entries []blocklistEntry
	seen := map[string]struct{}{}
	pendingNote := ""
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(raw, "#") {
			pendingNote = strings.TrimSpace(strings.TrimPrefix(raw, "#"))
			continue
		}
		line, inlineNote := splitBlocklistLine(raw)
		if line == "" {
			continue
		}
		prefix, err := parseBlockPrefix(line)
		if err != nil {
			continue
		}
		key := prefix.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		note := inlineNote
		if note == "" {
			note = pendingNote
		}
		entries = append(entries, blocklistEntry{prefix: prefix, source: line, note: note})
		pendingNote = ""
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取黑名单订阅 %q 失败: %w", feedURL, err)
	}
	return entries, nil
}

func splitBlocklistLine(raw string) (string, string) {
	before, after, ok := strings.Cut(raw, "#")
	line := strings.TrimSpace(before)
	if !ok {
		return line, ""
	}
	return line, strings.TrimSpace(after)
}
