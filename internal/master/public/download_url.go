package public

import "strings"

func joinDownloadURL(baseURL, assetID string) string {
	return strings.TrimRight(baseURL, "/") + "/downloads/" + assetID
}
