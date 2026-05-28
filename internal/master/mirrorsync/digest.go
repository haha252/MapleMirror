package mirrorsync

import (
	"fmt"
	"regexp"
	"strings"
)

var sha256Digest = regexp.MustCompile(`(?i)^sha256:([0-9a-f]{64})$`)

func normalizeDigest(value string) (string, error) {
	matches := sha256Digest.FindStringSubmatch(strings.TrimSpace(value))
	if matches == nil {
		return "", fmt.Errorf("GitHub asset 摘要缺失或不是 sha256")
	}
	return "sha256:" + strings.ToLower(matches[1]), nil
}
