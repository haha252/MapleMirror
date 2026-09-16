package v2

// WholeSource is a whole-file verified peer used when piece Swarm is disabled
// (for example after a manifest conflict or for assets above the Swarm layout
// limit). It deliberately mirrors the legacy replication capability so the
// Node can reuse the hardened whole-file fallback implementation.
type WholeSource struct {
	NodeID      string      `json:"node_id"`
	NodeName    string      `json:"node_name,omitempty"`
	DownloadURL string      `json:"download_url"`
	Token       string      `json:"token"`
	Parts       []WholePart `json:"parts,omitempty"`
}

type WholePart struct {
	RangeStart int64  `json:"range_start"`
	RangeEnd   int64  `json:"range_end"`
	Token      string `json:"token"`
}
