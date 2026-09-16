package syncer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"mirror-server/internal/protocol"
)

func (e Executor) fetchPeerParts(ctx context.Context, task protocol.SyncTask,
	source protocol.SyncFallbackSource, tmpPath string) (string, int64, error) {
	if err := validatePeerParts(task.Asset.SizeBytes, source.Parts); err != nil {
		return "", 0, err
	}
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return "", 0, err
	}
	if err := file.Truncate(task.Asset.SizeBytes); err != nil {
		_ = file.Close()
		return "", 0, err
	}
	workers := e.effectivePeerFallbackWorkers()
	if workers > len(source.Parts) {
		workers = len(source.Parts)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan protocol.SyncFallbackPart)
	errs := make(chan error, 1)
	limiter := e.sharedRateLimiter()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for part := range jobs {
				if err := e.fetchPeerPart(ctx, source.DownloadURL, part, file, limiter); err != nil {
					select {
					case errs <- err:
						cancel()
					default:
					}
					return
				}
			}
		}()
	}
	sentAll := true
sendLoop:
	for _, part := range source.Parts {
		select {
		case jobs <- part:
		case <-ctx.Done():
			sentAll = false
			break sendLoop
		}
	}
	close(jobs)
	wg.Wait()
	if closeErr := file.Close(); closeErr != nil {
		return "", 0, closeErr
	}
	select {
	case err := <-errs:
		return "", 0, err
	default:
	}
	if !sentAll {
		return "", 0, ctx.Err()
	}
	return fileDigest(tmpPath)
}

func (e Executor) fetchPeerPart(ctx context.Context, rawURL string,
	part protocol.SyncFallbackPart, file *os.File, limiter *BandwidthLimiter) error {
	if err := validateSourceURL(rawURL, e.AllowPrivateSourceURLs); err != nil {
		return err
	}
	client := e.effectiveSourceClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+part.Token)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", part.RangeStart, part.RangeEnd))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("分片复制响应异常: %s", resp.Status)
	}
	want := part.RangeEnd - part.RangeStart + 1
	if resp.ContentLength >= 0 && resp.ContentLength != want {
		return fmt.Errorf("分片复制大小不匹配")
	}
	body := io.Reader(resp.Body)
	if limiter != nil {
		body = &rateLimitedReader{reader: resp.Body, shared: limiter}
	}
	w := io.NewOffsetWriter(file, part.RangeStart)
	n, err := io.Copy(w, body)
	if err != nil {
		return err
	}
	if n != want {
		return fmt.Errorf("分片复制大小不匹配")
	}
	return nil
}

func (e Executor) effectivePeerFallbackWorkers() int {
	if e.PeerFallbackWorkers > 0 {
		return e.PeerFallbackWorkers
	}
	return 8
}

func validatePeerParts(size int64, parts []protocol.SyncFallbackPart) error {
	if size <= 0 || len(parts) == 0 {
		return errors.New("分片复制任务不完整")
	}
	var next int64
	for _, part := range parts {
		if part.Token == "" || part.RangeStart != next || part.RangeEnd < part.RangeStart || part.RangeEnd >= size {
			return errors.New("分片复制范围不合法")
		}
		next = part.RangeEnd + 1
	}
	if next != size {
		return errors.New("分片复制范围不完整")
	}
	return nil
}
