package control

import (
	"context"

	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func (r Repository) v2WholeSources(ctx context.Context, targetNodeID string, task protocolv2.SyncTask) []protocolv2.WholeSource {
	legacy := protocol.SyncTask{TaskID: task.TaskID, TaskType: task.TaskType, Asset: protocol.SyncAsset{
		AssetID: task.Asset.AssetID, ProjectID: task.Asset.ProjectID, Version: task.Asset.Version,
		FileName: task.Asset.FileName, SizeBytes: task.Asset.SizeBytes,
		DownloadURL: task.Asset.DownloadURL, DigestSHA256: task.Asset.DigestSHA256,
	}}
	in := r.syncFallbackSources(ctx, targetNodeID, legacy)
	out := make([]protocolv2.WholeSource, 0, len(in))
	for _, source := range in {
		parts := make([]protocolv2.WholePart, 0, len(source.Parts))
		for _, part := range source.Parts {
			parts = append(parts, protocolv2.WholePart{RangeStart: part.RangeStart, RangeEnd: part.RangeEnd, Token: part.Token})
		}
		out = append(out, protocolv2.WholeSource{NodeID: source.NodeID, NodeName: source.NodeName,
			DownloadURL: source.DownloadURL, Token: source.Token, Parts: parts})
	}
	return out
}
