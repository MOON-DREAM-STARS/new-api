package common

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type cgroupCPUPaths struct {
	version    int
	usagePath  string
	quotaPath  string
	periodPath string
}

type containerCPUReader struct {
	paths        cgroupCPUPaths
	fallbackCPUs int
	lastUsage    uint64
	lastSampled  time.Time
	initialized  bool
}

func newContainerCPUReader() *containerCPUReader {
	return &containerCPUReader{
		paths:        discoverCgroupCPUPaths(),
		fallbackCPUs: runtime.NumCPU(),
	}
}

// sample returns container CPU use relative to its available CPU capacity.
// available=false means container-scoped metrics are missing or unreadable.
// ready=false means the reader needs another sample before it can calculate a rate.
func (r *containerCPUReader) sample(now time.Time) (percent float64, available bool, ready bool) {
	if r == nil || r.paths.usagePath == "" {
		return 0, false, false
	}

	usageUsec, ok := r.paths.readUsageUsec()
	if !ok {
		return 0, false, false
	}

	capacity, ok := r.paths.readCapacityCores()
	if !ok {
		return 0, false, false
	}
	if capacity <= 0 {
		capacity = float64(r.fallbackCPUs)
	}
	if capacity <= 0 {
		return 0, false, false
	}

	if !r.initialized || usageUsec < r.lastUsage || !now.After(r.lastSampled) {
		r.lastUsage = usageUsec
		r.lastSampled = now
		r.initialized = true
		return 0, true, false
	}

	elapsed := now.Sub(r.lastSampled)
	if elapsed < time.Second {
		return 0, true, false
	}

	usedCores := float64(usageUsec-r.lastUsage) / 1e6 / elapsed.Seconds()
	r.lastUsage = usageUsec
	r.lastSampled = now
	return usedCores / capacity * 100, true, true
}

func (p cgroupCPUPaths) readUsageUsec() (uint64, bool) {
	raw, err := os.ReadFile(p.usagePath)
	if err != nil {
		return 0, false
	}

	if p.version == 2 {
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "usage_usec" {
				value, err := strconv.ParseUint(fields[1], 10, 64)
				return value, err == nil
			}
		}
		return 0, false
	}

	value, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0, false
	}
	return value / 1000, true
}

func (p cgroupCPUPaths) readCapacityCores() (float64, bool) {
	if p.version == 2 {
		raw, err := os.ReadFile(p.quotaPath)
		if err != nil {
			return 0, true
		}
		fields := strings.Fields(string(raw))
		if len(fields) < 2 || strings.EqualFold(fields[0], "max") {
			return 0, true
		}
		quota, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || quota <= 0 {
			return 0, true
		}
		period, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || period <= 0 {
			return 0, true
		}
		return quota / period, true
	}

	quotaRaw, err := os.ReadFile(p.quotaPath)
	if err != nil {
		return 0, true
	}
	quota, err := strconv.ParseFloat(strings.TrimSpace(string(quotaRaw)), 64)
	if err != nil || quota <= 0 {
		return 0, true
	}
	periodRaw, err := os.ReadFile(p.periodPath)
	if err != nil {
		return 0, true
	}
	period, err := strconv.ParseFloat(strings.TrimSpace(string(periodRaw)), 64)
	if err != nil || period <= 0 {
		return 0, true
	}
	return quota / period, true
}

func discoverCgroupCPUPaths() cgroupCPUPaths {
	cgroupData, _ := os.ReadFile("/proc/self/cgroup")
	mountInfo, _ := os.ReadFile("/proc/self/mountinfo")
	candidate := discoverCgroupCPUPathsFrom(string(cgroupData), string(mountInfo), "/sys/fs/cgroup")
	if candidate.usagePath != "" {
		if _, err := os.Stat(candidate.usagePath); err == nil {
			return candidate
		}
	}

	for _, fallback := range fallbackCgroupCPUPaths("/sys/fs/cgroup") {
		if _, err := os.Stat(fallback.usagePath); err == nil {
			return fallback
		}
	}
	return cgroupCPUPaths{}
}

func discoverCgroupCPUPathsFrom(cgroupData, mountInfo, sysRoot string) cgroupCPUPaths {
	if cgroupPath, ok := parseCgroupV2Path(cgroupData); ok {
		if mount, found := findCgroupMount(mountInfo, "cgroup2", ""); found {
			root := resolveCgroupMountPath(mount.mountPoint, mount.mountRoot, cgroupPath)
			return cgroupCPUPaths{
				version:   2,
				usagePath: filepath.Join(root, "cpu.stat"),
				quotaPath: filepath.Join(root, "cpu.max"),
			}
		}
	}

	if cgroupPath, ok := parseCgroupV1Path(cgroupData); ok {
		usageRoot := filepath.Join(sysRoot, "cpuacct")
		quotaRoot := filepath.Join(sysRoot, "cpu")
		if mount, found := findCgroupMount(mountInfo, "cgroup", "cpuacct"); found {
			usageRoot = resolveCgroupMountPath(mount.mountPoint, mount.mountRoot, cgroupPath)
		}
		if mount, found := findCgroupMount(mountInfo, "cgroup", "cpu"); found {
			quotaRoot = resolveCgroupMountPath(mount.mountPoint, mount.mountRoot, cgroupPath)
		}
		return cgroupCPUPaths{
			version:    1,
			usagePath:  filepath.Join(usageRoot, "cpuacct.usage"),
			quotaPath:  filepath.Join(quotaRoot, "cpu.cfs_quota_us"),
			periodPath: filepath.Join(quotaRoot, "cpu.cfs_period_us"),
		}
	}

	return cgroupCPUPaths{}
}

func fallbackCgroupCPUPaths(sysRoot string) []cgroupCPUPaths {
	v2Root := sysRoot
	v1CombinedRoot := filepath.Join(sysRoot, "cpu,cpuacct")
	return []cgroupCPUPaths{
		{version: 2, usagePath: filepath.Join(v2Root, "cpu.stat"), quotaPath: filepath.Join(v2Root, "cpu.max")},
		{version: 1, usagePath: filepath.Join(v1CombinedRoot, "cpuacct.usage"), quotaPath: filepath.Join(v1CombinedRoot, "cpu.cfs_quota_us"), periodPath: filepath.Join(v1CombinedRoot, "cpu.cfs_period_us")},
		{version: 1, usagePath: filepath.Join(sysRoot, "cpuacct", "cpuacct.usage"), quotaPath: filepath.Join(sysRoot, "cpu", "cpu.cfs_quota_us"), periodPath: filepath.Join(sysRoot, "cpu", "cpu.cfs_period_us")},
	}
}

type cgroupMount struct {
	mountPoint string
	mountRoot  string
	options    string
}

func findCgroupMount(mountInfo, fsType, controller string) (cgroupMount, bool) {
	for _, line := range strings.Split(mountInfo, "\n") {
		fields := strings.Fields(line)
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if separator < 0 || separator+2 >= len(fields) || fields[separator+1] != fsType || len(fields) < 5 {
			continue
		}
		mount := cgroupMount{
			mountPoint: fields[4],
			mountRoot:  fields[3],
			options:    fields[separator+3],
		}
		if controller == "" || mountHasController(mount, controller) {
			return mount, true
		}
	}
	return cgroupMount{}, false
}

func mountHasController(mount cgroupMount, controller string) bool {
	for _, token := range strings.Split(mount.options, ",") {
		if strings.TrimSpace(token) == controller {
			return true
		}
	}
	return false
}

func parseCgroupV2Path(cgroupData string) (string, bool) {
	for _, line := range strings.Split(cgroupData, "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) == 3 && fields[0] == "0" && fields[1] == "" && fields[2] != "" {
			return fields[2], true
		}
	}
	return "", false
}

func parseCgroupV1Path(cgroupData string) (string, bool) {
	for _, line := range strings.Split(cgroupData, "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 {
			continue
		}
		for _, controller := range strings.Split(fields[1], ",") {
			if controller == "cpu" || controller == "cpuacct" {
				return fields[2], true
			}
		}
	}
	return "", false
}

func resolveCgroupMountPath(mountPoint, mountRoot, cgroupPath string) string {
	cgroupPath = "/" + strings.TrimPrefix(filepath.ToSlash(cgroupPath), "/")
	mountRoot = "/" + strings.TrimPrefix(filepath.ToSlash(mountRoot), "/")
	relative := cgroupPath
	if mountRoot != "/" {
		if cgroupPath == mountRoot {
			relative = "/"
		} else if strings.HasPrefix(cgroupPath, mountRoot+"/") {
			relative = strings.TrimPrefix(cgroupPath, mountRoot)
		}
	}
	return filepath.Join(mountPoint, filepath.FromSlash(strings.TrimPrefix(relative, "/")))
}
