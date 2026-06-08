//go:build !linux && !windows

package control

import "errors"

func readNonLoopbackNetworkBytes() (uint64, error) {
	return 0, errors.New("network bandwidth sampling is not supported on this platform")
}
