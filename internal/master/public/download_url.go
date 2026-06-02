package public

import (
	"strings"

	"mirror-server/internal/assetpath"
)

func joinDownloadURL(baseURL, projectID, version, fileName string) string {
	return strings.TrimRight(baseURL, "/") + assetpath.PublicPath(projectID, version, fileName)
}
