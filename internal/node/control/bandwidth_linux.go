//go:build linux

package control

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func readNonLoopbackNetworkBytes() (uint64, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var total uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		name, stats, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "lo" {
			continue
		}
		parts := strings.Fields(stats)
		if len(parts) < 16 {
			continue
		}
		tx, err := strconv.ParseUint(parts[8], 10, 64)
		if err != nil {
			continue
		}
		total += tx
	}
	return total, scanner.Err()
}
