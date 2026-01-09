package runnercontainer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/pslog"
)

func ResourceCapsFromPercent(cpuPercent, memoryPercent int, logger pslog.Logger) *shipohoy.ResourceCaps {
	var caps shipohoy.ResourceCaps
	if cpuNano, ok := nanoCPUsFromPercent(runtime.NumCPU(), cpuPercent); ok {
		caps.NanoCPUs = cpuNano
	}
	if memBytes, ok := memoryBytesFromPercent(memoryPercent); ok {
		caps.MemoryBytes = memBytes
	}
	if caps.NanoCPUs == 0 && caps.MemoryBytes == 0 {
		return nil
	}
	if logger != nil {
		logger.Info("runner resource caps", "cpu_nano", caps.NanoCPUs, "memory_bytes", caps.MemoryBytes)
	}
	return &caps
}

func nanoCPUsFromPercent(cpuCount int, percent int) (int64, bool) {
	if cpuCount <= 0 || percent <= 0 {
		return 0, false
	}
	if percent > 100 {
		percent = 100
	}
	cpus := float64(cpuCount) * float64(percent) / 100.0
	if cpus <= 0 {
		return 0, false
	}
	nano := int64(cpus*1e9 + 0.5)
	if nano <= 0 {
		return 0, false
	}
	return nano, true
}

func memoryBytesFromPercent(percent int) (int64, bool) {
	total, err := readMemTotalBytes()
	if err != nil {
		return 0, false
	}
	return memoryBytesFromPercentWithTotal(total, percent)
}

func memoryBytesFromPercentWithTotal(total int64, percent int) (int64, bool) {
	if percent <= 0 {
		return 0, false
	}
	if percent > 100 {
		percent = 100
	}
	if total <= 0 {
		return 0, false
	}
	limit := total * int64(percent) / 100
	if limit <= 0 {
		return 0, false
	}
	return limit, true
}

func readMemTotalBytes() (int64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()
	return parseMemTotalBytes(file)
}

func parseMemTotalBytes(r io.Reader) (int64, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, errors.New("meminfo: invalid MemTotal line")
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		unit := ""
		if len(fields) >= 3 {
			unit = fields[2]
		}
		switch unit {
		case "kB", "KB", "":
			return value * 1024, nil
		default:
			return 0, fmt.Errorf("meminfo: unsupported unit %q", unit)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("meminfo: MemTotal not found")
}
