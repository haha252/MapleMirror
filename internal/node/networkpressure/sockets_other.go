//go:build !linux

package networkpressure

import "errors"

func readSockets() ([]socketSample, error) {
	return nil, errors.New("TCP delivery sampling is only supported on Linux")
}
