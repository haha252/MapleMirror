package control

import (
	"encoding/json"

	"mirror-server/internal/protocol"
)

const inventoryChunkMaxPayloadBytes = protocol.MaxFrameBytes / 2

func inventoryChunks(items []protocol.InventoryItem) [][]protocol.InventoryItem {
	if len(items) == 0 {
		return [][]protocol.InventoryItem{{}}
	}
	chunks := make([][]protocol.InventoryItem, 0, (len(items)+inventoryChunkSize-1)/inventoryChunkSize)
	var chunk []protocol.InventoryItem
	chunkBytes := 2
	for _, item := range items {
		itemBytes := inventoryItemJSONSize(item)
		separatorBytes := 0
		if len(chunk) > 0 {
			separatorBytes = 1
		}
		if len(chunk) > 0 &&
			(len(chunk) >= inventoryChunkSize ||
				chunkBytes+separatorBytes+itemBytes > inventoryChunkMaxPayloadBytes) {
			chunks = append(chunks, chunk)
			chunk = nil
			chunkBytes = 2
			separatorBytes = 0
		}
		chunk = append(chunk, item)
		chunkBytes += separatorBytes + itemBytes
	}
	if len(chunk) > 0 {
		chunks = append(chunks, chunk)
	}
	return chunks
}

func inventoryItemJSONSize(item protocol.InventoryItem) int {
	data, err := json.Marshal(item)
	if err != nil {
		return inventoryChunkMaxPayloadBytes
	}
	return len(data)
}
