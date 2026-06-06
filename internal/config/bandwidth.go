package config

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

func parseBandwidthBPS(value string, allowZero bool) (int64, error) {
	value = strings.TrimSpace(value)
	if allowZero && value == "0" {
		return 0, nil
	}
	number, unit, ok := splitBandwidth(value)
	if !ok {
		return 0, errors.New("invalid bandwidth format")
	}
	count, err := strconv.ParseFloat(number, 64)
	if err != nil || count <= 0 || math.IsInf(count, 0) || math.IsNaN(count) {
		return 0, errors.New("invalid bandwidth number")
	}
	factor, ok := bandwidthUnitFactor(unit)
	if !ok {
		return 0, errors.New("invalid bandwidth unit")
	}
	bps := count * factor
	if bps < 1 || bps > float64(math.MaxInt64) {
		return 0, errors.New("invalid bandwidth value")
	}
	return int64(bps), nil
}

func splitBandwidth(value string) (string, string, bool) {
	parts := strings.Fields(value)
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	if len(parts) != 1 {
		return "", "", false
	}
	for _, unit := range []string{"MiB/s", "MB/s", "Mbps"} {
		if strings.HasSuffix(parts[0], unit) {
			number := strings.TrimSpace(strings.TrimSuffix(parts[0], unit))
			return number, unit, number != ""
		}
	}
	return "", "", false
}

func bandwidthUnitFactor(unit string) (float64, bool) {
	switch unit {
	case "MiB/s":
		return 1024 * 1024, true
	case "MB/s":
		return 1000 * 1000, true
	case "Mbps":
		return 1000 * 1000 / 8, true
	default:
		return 0, false
	}
}
