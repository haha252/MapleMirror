package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
)

func (e Executor) fetch(ctx context.Context, url, tmpPath string) (string, int64, error) {
	client := e.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("下载响应异常: %s", resp.Status)
	}
	file, err := os.Create(tmpPath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), resp.Body)
	if err != nil {
		return "", size, err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), size, nil
}
