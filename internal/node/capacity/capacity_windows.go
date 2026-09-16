//go:build windows

package capacity

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func samplePath(path string) Sample {
	path = nearestExisting(path)
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Sample{}
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &available, &total, &free); err != nil {
		return Sample{}
	}
	volume := strings.ToLower(filepath.VolumeName(path))
	return Sample{AvailableBytes: int64(available), TotalBytes: int64(total), Valid: true, filesystemID: volume}
}
