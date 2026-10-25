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
	if r.fallbackCPUs > 0 {
		capacity = min(capacity, float64(r.fallbackCPUs))
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

// 容器自身资源采样。
//
// 历史实现直接读取宿主机口径（/proc/stat 与 /proc/meminfo），把同节点其它
// 工作负载的占用也算成本进程过载，导致正常的 relay 流量被 503 熔断。这里
// 改为优先读取当前容器的 cgroup 配额与用量，只有在拿不到 cgroup 数据时才由
// 调用方回退到宿主机口径。

const (
	defaultCgroupRoot = "/sys/fs/cgroup"
	// cgroup v1 用接近 int64 上限的值表示"未限制"。
	unlimitedMemoryBytes = uint64(1) << 60
)

// containerUsage 记录上一次采样点，用来计算区间内的 CPU 占用。
type containerUsage struct {
	cpuReader  *containerCPUReader
	cgroupRoot string
	// schedulableCPUs 是本进程可调度的逻辑 CPU 数（Linux 上含 cpuset/affinity 限制）。
	schedulableCPUs float64
	cpuLastUsec     uint64
	cpuLastSampled  time.Time
}

func newContainerUsage() *containerUsage {
	// runtime.NumCPU 在 Linux 上于进程启动时按 sched_getaffinity 计数，不受 GOMAXPROCS 影响。
	return &containerUsage{cgroupRoot: defaultCgroupRoot, schedulableCPUs: float64(runtime.NumCPU()), cpuReader: newContainerCPUReader()}
}

// cpuSaturation 返回相对容器可用 CPU 的使用率（百分比）。
// 可用 CPU 取 cgroup 配额与可调度 CPU 的较小者：配额大于 cpuset 或节点核数时，
// 按配额计算会让容器打满也无法触发保护。
// ok=false 表示当前环境没有可用的 cgroup CPU 数据，调用方应回退到宿主机口径。
func (u *containerUsage) cpuSaturation(now time.Time) (float64, bool) {
	if u.cpuReader != nil {
		percent, available, ready := u.cpuReader.sample(now)
		return percent, available && ready
	}
	limitCores, ok := u.readCgroupCPUQuotaCores()
	if !ok {
		return 0, false
	}
	if u.schedulableCPUs > 0 {
		limitCores = min(limitCores, u.schedulableCPUs)
	}
	usageUsec, ok := u.readCgroupCPUUsageUsec()
	if !ok {
		return 0, false
	}
	if u.cpuLastSampled.IsZero() || usageUsec < u.cpuLastUsec || !now.After(u.cpuLastSampled) {
		u.cpuLastUsec = usageUsec
		u.cpuLastSampled = now
		return 0, false
	}
	elapsed := now.Sub(u.cpuLastSampled)
	if elapsed < time.Second {
		return 0, false
	}
	deltaUsec := usageUsec - u.cpuLastUsec
	u.cpuLastUsec = usageUsec
	u.cpuLastSampled = now

	usedCores := float64(deltaUsec) / 1e6 / elapsed.Seconds()
	return usedCores / limitCores * 100, true
}

// memoryUsage 返回容器内存工作集相对上限的使用率（百分比）。
// 工作集 = 用量 - 非活跃文件缓存，与 docker stats、kubelet 一致：用量包含可回收的
// 页缓存，写日志或文件后会长期贴近上限，不代表内存压力。
// ok=false 时调用方回退宿主机口径（同样不含缓存）：内存未限制、文件不可读、
// 或 memory.stat 缺少非活跃缓存字段。宁可回退旧口径，也不把含缓存的用量当作压力。
func (u *containerUsage) memoryUsage() (float64, bool) {
	usagePath := filepath.Join(u.cgroupRoot, "memory.current")
	limitPath := filepath.Join(u.cgroupRoot, "memory.max")
	statPath := filepath.Join(u.cgroupRoot, "memory.stat")
	inactiveFileKey := "inactive_file"
	if _, err := os.Stat(usagePath); err != nil {
		v1Dir := filepath.Join(u.cgroupRoot, "memory")
		usagePath = filepath.Join(v1Dir, "memory.usage_in_bytes")
		limitPath = filepath.Join(v1Dir, "memory.limit_in_bytes")
		statPath = filepath.Join(v1Dir, "memory.stat")
		inactiveFileKey = "total_inactive_file"
	}

	// cgroup v2 未限制时 memory.max 为 "max"，解析失败即视为不可用。
	limit, ok := readCgroupUint(limitPath)
	if !ok || limit == 0 || limit >= unlimitedMemoryBytes {
		return 0, false
	}
	usage, ok := readCgroupUint(usagePath)
	if !ok {
		return 0, false
	}
	inactiveFile, ok := readCgroupStatValue(statPath, inactiveFileKey)
	if !ok {
		return 0, false
	}
	// 用量与 memory.stat 不是同一时刻的快照，缓存可能短暂大于用量，按 0 处理防止下溢。
	workingSet := usage - min(inactiveFile, usage)
	return float64(workingSet) / float64(limit) * 100, true
}

// readCgroupCPUQuotaCores 返回 cgroup CPU 配额（核）。ok=false 表示未限制或不可读。
func (u *containerUsage) readCgroupCPUQuotaCores() (float64, bool) {
	if raw, err := os.ReadFile(filepath.Join(u.cgroupRoot, "cpu.max")); err == nil {
		quotaText, periodText, found := strings.Cut(strings.TrimSpace(string(raw)), " ")
		if !found || quotaText == "max" {
			return 0, false
		}
		quota, err := strconv.ParseUint(quotaText, 10, 64)
		if err != nil || quota == 0 {
			return 0, false
		}
		period, err := strconv.ParseUint(strings.TrimSpace(periodText), 10, 64)
		if err != nil || period == 0 {
			return 0, false
		}
		return float64(quota) / float64(period), true
	}

	// cgroup v1 未限制时 cfs_quota_us 为 -1，解析失败即视为不可用。
	v1Dir := filepath.Join(u.cgroupRoot, "cpu")
	quota, ok := readCgroupUint(filepath.Join(v1Dir, "cpu.cfs_quota_us"))
	if !ok || quota == 0 {
		return 0, false
	}
	period, ok := readCgroupUint(filepath.Join(v1Dir, "cpu.cfs_period_us"))
	if !ok || period == 0 {
		return 0, false
	}
	return float64(quota) / float64(period), true
}

func (u *containerUsage) readCgroupCPUUsageUsec() (uint64, bool) {
	statPath := filepath.Join(u.cgroupRoot, "cpu.stat")
	if _, err := os.Stat(statPath); err == nil {
		return readCgroupStatValue(statPath, "usage_usec")
	}
	// cgroup v1 的 cpuacct.usage 单位是纳秒。
	nanos, ok := readCgroupUint(filepath.Join(u.cgroupRoot, "cpuacct", "cpuacct.usage"))
	return nanos / 1000, ok
}

// readCgroupUint 读取只含一个无符号整数的 cgroup 文件。
func readCgroupUint(path string) (uint64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	return value, err == nil
}

// readCgroupStatValue 从 "key value" 逐行格式的 cgroup 统计文件（cpu.stat、memory.stat）中读取一项。
func readCgroupStatValue(path string, key string) (uint64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		name, valueText, found := strings.Cut(line, " ")
		if !found || name != key {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimSpace(valueText), 10, 64)
		return value, err == nil
	}
	return 0, false
}
