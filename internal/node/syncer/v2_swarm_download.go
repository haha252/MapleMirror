package syncer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func (e Executor) orderMissingPieces(m protocolv2.SwarmManifest, bits []byte) []int {
	type item struct{ i, n int }
	var items []item
	sources := []protocolv2.SwarmSource(nil)
	if e.Swarm != nil {
		sources = e.Swarm.Sources(m.ManifestID)
	}
	for i := 0; i < m.PieceCount; i++ {
		if swarm.Has(bits, i) {
			continue
		}
		n := 0
		for _, s := range sources {
			if swarm.Has(s.Availability, i) {
				n++
			}
		}
		items = append(items, item{i, n})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].n < items[j].n })
	out := make([]int, len(items))
	for i, x := range items {
		out[i] = x.i
	}
	return out
}

func (e Executor) downloadSwarmPiece(ctx context.Context, task protocolv2.SyncTask, m protocolv2.SwarmManifest, index int, file *os.File) error {
	start, end, ok := swarm.PieceBounds(m.AssetSize, m.PieceSize, index)
	if !ok {
		return errors.New("piece bounds invalid")
	}
	for off := start; off < end; {
		blockEnd := off + swarm.BlockSize
		if blockEnd > end {
			blockEnd = end
		}
		if err := e.fetchSwarmBlock(ctx, task, m, index, off, blockEnd, file); err != nil {
			return err
		}
		off = blockEnd
	}
	h := sha256.New()
	section := io.NewSectionReader(file, start, end-start)
	if _, err := io.Copy(h, section); err != nil {
		return err
	}
	want := m.PieceHashes[index*32 : (index+1)*32]
	if !bytes.Equal(h.Sum(nil), want) {
		return fmt.Errorf("piece %d hash mismatch", index)
	}
	return nil
}

func (e Executor) fetchSwarmBlock(ctx context.Context, task protocolv2.SyncTask, m protocolv2.SwarmManifest, piece int, start, end int64, file *os.File) error {
	// A peer-only node needs time for cooldowns and refreshed capabilities.
	// Bound recovery by both the task deadline and a per-block deadline.
	if e.Swarm == nil || !e.ForcePeerDownload {
		return e.trySwarmBlock(ctx, task, m, piece, start, end, file)
	}
	retryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var last error
	for {
		last = e.trySwarmBlock(retryCtx, task, m, piece, start, end, file)
		if last == nil {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return fmt.Errorf("piece %d bytes %d-%d: %w (last source error: %v)", piece, start, end-1, retryCtx.Err(), last)
		case <-timer.C:
		}
	}
}

func (e Executor) trySwarmBlock(ctx context.Context, task protocolv2.SyncTask, m protocolv2.SwarmManifest, piece int, start, end int64, file *os.File) error {
	var sources []protocolv2.SwarmSource
	if e.Swarm != nil {
		sources = e.Swarm.Sources(m.ManifestID)
	} else {
		sources = task.Sources
	}
	var last error
	// Spread different pieces across otherwise-equivalent peers while keeping a
	// stable preferred source for every block of the same piece. A failed or
	// unavailable preferred peer still falls through to the remaining sources.
	for n := 0; n < len(sources); n++ {
		source := sources[(piece+n)%len(sources)]
		if !swarm.Has(source.Availability, piece) {
			continue
		}
		if e.Swarm != nil && !e.Swarm.SourceReady(m.ManifestID, source.NodeID) {
			continue
		}
		if err := e.fetchRangeInto(ctx, source.BaseURL, source.Capability, start, end, m.AssetSize, file); err == nil {
			if e.Swarm != nil {
				e.Swarm.ReportSourceSuccess(m.ManifestID, source.NodeID)
			}
			return nil
		} else {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			last = fmt.Errorf("peer %s: %w", source.NodeID, err)
			if e.Swarm != nil {
				e.Swarm.ReportSourceFailure(m.ManifestID, source.NodeID)
			}
		}
	}
	if !e.ForcePeerDownload && task.Asset.DownloadURL != "" {
		if err := e.fetchRangeInto(ctx, task.Asset.DownloadURL, "", start, end, m.AssetSize, file); err == nil {
			return nil
		} else {
			last = err
		}
	}
	if last == nil {
		last = errors.New("no source owns piece")
	}
	return last
}

const swarmBlockTimeout = 60 * time.Second
const swarmHeaderTimeout = 10 * time.Second

func (e Executor) fetchRangeInto(ctx context.Context, rawURL, token string, start, end, total int64, file *os.File) error {
	if err := validateSourceURL(rawURL, e.AllowPrivateSourceURLs); err != nil {
		return err
	}
	client := e.effectiveSourceClient()
	blockCtx, cancel := context.WithTimeout(ctx, swarmBlockTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(blockCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end-1))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("range source status %s", resp.Status)
	}
	if len(resp.Header.Values("Content-Range")) != 1 {
		return errors.New("range response must contain exactly one Content-Range")
	}
	if err := validateSwarmContentRange(resp.Header.Get("Content-Range"), start, end, total); err != nil {
		return err
	}
	want := end - start
	if resp.ContentLength >= 0 && resp.ContentLength != want {
		return errors.New("range content length mismatch")
	}
	body := io.Reader(resp.Body)
	if e.SyncLimiter != nil {
		body = &rateLimitedReader{reader: body, shared: e.SyncLimiter}
	}
	w := io.NewOffsetWriter(file, start)
	n, err := io.CopyN(w, body, want)
	if err != nil {
		return err
	}
	if n != want {
		return io.ErrUnexpectedEOF
	}
	var extra [1]byte
	extraN, extraErr := body.Read(extra[:])
	if extraN != 0 {
		return errors.New("range response body exceeds requested length")
	}
	if extraErr != nil && extraErr != io.EOF {
		return extraErr
	}
	if extraErr == nil {
		return errors.New("range response body did not terminate at requested length")
	}
	return nil
}
