package syncer

import (
	"context"
	"sync"

	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func (e Executor) ExecuteV2(ctx context.Context, task protocolv2.SyncTask) (protocolv2.SyncResult, *protocolv2.SwarmManifest) {
	legacy := legacyTaskFromV2(task)
	if task.TaskType != "asset_download" || task.SwarmDisabled {
		r := e.Execute(ctx, legacy)
		return v2ResultFromLegacy(task, r), nil
	}
	if task.Manifest == nil || task.Bootstrap {
		return e.executeV2Bootstrap(ctx, task, legacy)
	}
	return e.executeV2Swarm(ctx, task, legacy)
}

func legacyTaskFromV2(task protocolv2.SyncTask) protocol.SyncTask {
	out := protocol.SyncTask{TaskID: task.TaskID, TaskType: task.TaskType, Asset: protocol.SyncAsset{
		AssetID: task.Asset.AssetID, ProjectID: task.Asset.ProjectID, Version: task.Asset.Version, FileName: task.Asset.FileName,
		SizeBytes: task.Asset.SizeBytes, DownloadURL: task.Asset.DownloadURL, DigestSHA256: task.Asset.DigestSHA256,
	}}
	for _, source := range task.WholeSources {
		parts := make([]protocol.SyncFallbackPart, 0, len(source.Parts))
		for _, part := range source.Parts {
			parts = append(parts, protocol.SyncFallbackPart{RangeStart: part.RangeStart, RangeEnd: part.RangeEnd, Token: part.Token})
		}
		out.FallbackSources = append(out.FallbackSources, protocol.SyncFallbackSource{
			NodeID: source.NodeID, NodeName: source.NodeName, DownloadURL: source.DownloadURL, Token: source.Token, Parts: parts,
		})
	}
	return out
}

func v2ResultFromLegacy(task protocolv2.SyncTask, r protocol.SyncTaskResult) protocolv2.SyncResult {
	return protocolv2.SyncResult{TaskID: task.TaskID, AttemptID: task.AttemptID, AssetID: r.AssetID, Result: r.Result,
		LocalDigestSHA256: r.LocalDigestSHA256, SizeBytes: r.SizeBytes, Message: r.Message}
}

func (e Executor) executeV2Swarm(ctx context.Context, task protocolv2.SyncTask, legacy protocol.SyncTask) (protocolv2.SyncResult, *protocolv2.SwarmManifest) {
	m := *task.Manifest
	if e.Swarm != nil {
		e.Swarm.SetManifest(m)
		e.Swarm.SetSources(m.ManifestID, task.Sources)
	}
	if reused, ok := e.reuseVerifiedAsset(legacy); ok {
		crossCheck, err := e.manifestFromCommittedFile(task)
		if err != nil {
			return v2Failure(task, "temporary_error", "cross-check local manifest: "+err.Error()), nil
		}
		return v2ResultFromLegacy(task, reused), crossCheck
	}
	if e.Capacity != nil {
		release, err := e.Capacity.ReserveDownload(task.Asset.SizeBytes)
		if err != nil {
			return v2Failure(task, "temporary_error", "磁盘可用空间不足"), nil
		}
		defer release()
	}
	partial, bits, err := e.openSwarmPartial(task, m)
	if err != nil {
		return v2Failure(task, "temporary_error", err.Error()), nil
	}
	defer partial.Close()
	if swarm.Complete(bits, m.PieceCount) {
		return e.finishSwarmPartial(task, legacy, m, partial.Name(), bits)
	}
	missing := e.orderMissingPieces(m, bits)
	workers := e.effectivePeerFallbackWorkers()
	if workers > len(missing) {
		workers = len(missing)
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	errCh := make(chan error, 1)
	workCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	var wg sync.WaitGroup
	var bitMu sync.Mutex
	persistDone := make(chan struct{})
	go e.partialBitmapFlusher(workCtx, task, m, persistDone)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for piece := range jobs {
				if err := e.downloadSwarmPiece(workCtx, task, m, piece, partial); err != nil {
					select {
					case errCh <- err:
						cancelWorkers()
					default:
					}
					return
				}
				bitMu.Lock()
				swarm.Set(bits, piece)
				local := append([]byte(nil), bits...)
				bitMu.Unlock()
				if e.Swarm != nil {
					e.Swarm.MarkPiece(m.AssetID, m.ManifestID, piece)
					e.Swarm.UpdateBitset(m.AssetID, m.ManifestID, local)
				}
			}
		}()
	}
feed:
	for _, piece := range missing {
		select {
		case jobs <- piece:
		case <-workCtx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	cancelWorkers()
	<-persistDone
	select {
	case err := <-errCh:
		// If peers/range origin cannot complete, a full origin fetch remains the final safe fallback.
		if task.Asset.DownloadURL != "" {
			_ = partial.Close()
			return e.fullOriginFallbackV2(ctx, task, legacy, m, err)
		}
		return v2Failure(task, "temporary_error", err.Error()), nil
	default:
	}
	if err := e.persistPartialBitmap(task.Asset.AssetID, m.ManifestID, bits); err != nil {
		return v2Failure(task, "temporary_error", err.Error()), nil
	}
	return e.finishSwarmPartial(task, legacy, m, partial.Name(), bits)
}
