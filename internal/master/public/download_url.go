package public

import (
	"mirror-server/internal/assetpath"
	"mirror-server/internal/downloadurl"
)

func joinDownloadURL(baseURL, projectID, version, fileName string) (string, error) {
	return downloadurl.Join(baseURL, assetpath.PublicPath(projectID, version, fileName))
}
