package runnercontainer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"pkt.systems/pslog"
)

const cgroupPeriodUs int64 = 100000

func applyGroupLimits(cfg *Config, logger pslog.Logger) {
	if cfg == nil {
		return
	}
	parent := strings.TrimSpace(cfg.CgroupParent)
	if parent == "" {
		return
	}
	resolved, err := resolveCgroupParentPath(parent)
	if err != nil {
		logger.Warn("runner cgroup resolve failed", "parent", parent, "err", err)
		return
	}
	cfg.CgroupParent = resolved
	cgroupRoot := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(resolved, "/"))
	if err := os.MkdirAll(cgroupRoot, 0o755); err != nil {
		logger.Warn("runner cgroup create failed", "path", cgroupRoot, "err", err)
		return
	}

	if cpuMax, ok := cpuMaxFromPercent(runtime.NumCPU(), cfg.GroupCPUPercent); ok {
		writeCgroupValue(logger, filepath.Join(cgroupRoot, "cpu.max"), cpuMax)
	}
	if memMax, ok := memoryMaxFromPercent(cfg.GroupMemoryPercent); ok {
		writeCgroupValue(logger, filepath.Join(cgroupRoot, "memory.max"), memMax)
	}
}

func resolveCgroupParentPath(parent string) (string, error) {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return "", nil
	}
	current, err := currentCgroupPath()
	if err != nil {
		return "", err
	}
	return resolveCgroupParentPathWithCurrent(current, parent)
}

func resolveCgroupParentPathWithCurrent(current, parent string) (string, error) {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return "", nil
	}
	if strings.HasPrefix(parent, "/") {
		return path.Clean(parent), nil
	}
	return path.Clean(path.Join(current, parent)), nil
}

func currentCgroupPath() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "0::") {
			path := strings.TrimPrefix(line, "0::")
			if path == "" {
				path = "/"
			}
			return path, nil
		}
	}
	return "", errors.New("cgroup v2 not detected")
}

func cpuMaxFromPercent(cpuCount int, percent int) (string, bool) {
	if cpuCount <= 0 || percent <= 0 {
		return "", false
	}
	if percent > 100 {
		percent = 100
	}
	cpus := float64(cpuCount) * float64(percent) / 100.0
	if cpus <= 0 {
		return "", false
	}
	quota := int64(cpus*float64(cgroupPeriodUs) + 0.5)
	if quota <= 0 {
		return "", false
	}
	return fmt.Sprintf("%d %d", quota, cgroupPeriodUs), true
}

func memoryMaxFromPercent(percent int) (string, bool) {
	total, err := readMemTotalBytes()
	if err != nil {
		return "", false
	}
	return memoryMaxFromPercentWithTotal(total, percent)
}

func memoryMaxFromPercentWithTotal(total int64, percent int) (string, bool) {
	if percent <= 0 {
		return "", false
	}
	if percent > 100 {
		percent = 100
	}
	if total <= 0 {
		return "", false
	}
	limit := total * int64(percent) / 100
	if limit <= 0 {
		return "", false
	}
	return strconv.FormatInt(limit, 10), true
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

func writeCgroupValue(logger pslog.Logger, path string, value string) {
	if _, err := os.Stat(path); err != nil {
		logger.Warn("runner cgroup file missing", "path", path, "err", err)
		return
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		logger.Warn("runner cgroup write failed", "path", path, "err", err)
		return
	}
	logger.Info("runner cgroup limit set", "path", path, "value", value)
}
