package mirrorsync

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

func newID() (string, error) {
	data := make([]byte, 18)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("生成安全随机值失败：%w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
