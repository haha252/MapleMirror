//go:build !windows

package capacity

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func samplePath(path string) Sample {
	path = nearestExisting(path)
	var fs unix.Statfs_t
	if err := unix.Statfs(path, &fs); err != nil {
		return Sample{}
	}
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return Sample{}
	}
	block := int64(fs.Bsize)
	return Sample{AvailableBytes: int64(fs.Bavail) * block, TotalBytes: int64(fs.Blocks) * block,
		Valid: true, filesystemID: fmt.Sprintf("dev:%d", uint64(st.Dev))}
}
