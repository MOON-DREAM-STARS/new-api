package common

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverCgroupCPUPathsV2Nested(t *testing.T) {
	cgroupData := "0::/docker/abc\n"
	mountInfo := "36 25 0:32 / /sys/fs/cgroup rw,nosuid - cgroup2 cgroup rw\n"

	paths := discoverCgroupCPUPathsFrom(cgroupData, mountInfo, "/sys/fs/cgroup")

	assert.Equal(t, 2, paths.version)
	assert.Equal(t, filepath.Join("/sys/fs/cgroup", "docker", "abc", "cpu.stat"), paths.usagePath)
	assert.Equal(t, filepath.Join("/sys/fs/cgroup", "docker", "abc", "cpu.max"), paths.quotaPath)
}

func TestDiscoverCgroupCPUPathsV1CombinedMount(t *testing.T) {
	cgroupData := "5:cpu,cpuacct:/docker/abc\n"
	mountInfo := "36 25 0:32 / /sys/fs/cgroup/cpu,cpuacct rw,nosuid - cgroup cgroup rw,cpu,cpuacct\n"

	paths := discoverCgroupCPUPathsFrom(cgroupData, mountInfo, "/sys/fs/cgroup")

	assert.Equal(t, 1, paths.version)
	assert.Equal(t, filepath.Join("/sys/fs/cgroup/cpu,cpuacct", "docker", "abc", "cpuacct.usage"), paths.usagePath)
	assert.Equal(t, filepath.Join("/sys/fs/cgroup/cpu,cpuacct", "docker", "abc", "cpu.cfs_quota_us"), paths.quotaPath)
	assert.Equal(t, filepath.Join("/sys/fs/cgroup/cpu,cpuacct", "docker", "abc", "cpu.cfs_period_us"), paths.periodPath)
}

func TestContainerCPUReaderCalculatesContainerSaturation(t *testing.T) {
	dir := t.TempDir()
	usagePath := filepath.Join(dir, "cpu.stat")
	quotaPath := filepath.Join(dir, "cpu.max")
	require.NoError(t, os.WriteFile(usagePath, []byte("usage_usec 1000000\n"), 0o600))
	require.NoError(t, os.WriteFile(quotaPath, []byte("max 100000\n"), 0o600))

	reader := &containerCPUReader{
		paths:        cgroupCPUPaths{version: 2, usagePath: usagePath, quotaPath: quotaPath},
		fallbackCPUs: 2,
	}
	now := time.Unix(100, 0)
	_, available, ready := reader.sample(now)
	require.True(t, available)
	require.False(t, ready)

	require.NoError(t, os.WriteFile(usagePath, []byte("usage_usec 2000000\n"), 0o600))
	percent, available, ready := reader.sample(now.Add(5 * time.Second))
	require.True(t, available)
	require.True(t, ready)
	assert.InDelta(t, 10, percent, 0.001)
}

func TestContainerCPUReaderResetsOnCounterRegression(t *testing.T) {
	dir := t.TempDir()
	usagePath := filepath.Join(dir, "cpu.stat")
	quotaPath := filepath.Join(dir, "cpu.max")
	require.NoError(t, os.WriteFile(usagePath, []byte("usage_usec 1000000\n"), 0o600))
	require.NoError(t, os.WriteFile(quotaPath, []byte("100000 100000\n"), 0o600))

	reader := &containerCPUReader{
		paths:        cgroupCPUPaths{version: 2, usagePath: usagePath, quotaPath: quotaPath},
		fallbackCPUs: 1,
	}
	now := time.Unix(100, 0)
	_, available, ready := reader.sample(now)
	require.True(t, available)
	require.False(t, ready)

	require.NoError(t, os.WriteFile(usagePath, []byte("usage_usec 500000\n"), 0o600))
	_, available, ready = reader.sample(now.Add(5 * time.Second))
	require.True(t, available)
	require.False(t, ready)

	require.NoError(t, os.WriteFile(usagePath, []byte("usage_usec 1000000\n"), 0o600))
	percent, available, ready := reader.sample(now.Add(10 * time.Second))
	require.True(t, available)
	require.True(t, ready)
	assert.InDelta(t, 10, percent, 0.001)
}

func TestContainerCPUReaderV1Nanos(t *testing.T) {
	dir := t.TempDir()
	usagePath := filepath.Join(dir, "cpuacct.usage")
	quotaPath := filepath.Join(dir, "cpu.cfs_quota_us")
	periodPath := filepath.Join(dir, "cpu.cfs_period_us")
	require.NoError(t, os.WriteFile(usagePath, []byte("1000000000\n"), 0o600))
	require.NoError(t, os.WriteFile(quotaPath, []byte("100000\n"), 0o600))
	require.NoError(t, os.WriteFile(periodPath, []byte("100000\n"), 0o600))

	reader := &containerCPUReader{
		paths:        cgroupCPUPaths{version: 1, usagePath: usagePath, quotaPath: quotaPath, periodPath: periodPath},
		fallbackCPUs: 1,
	}
	now := time.Unix(100, 0)
	_, available, ready := reader.sample(now)
	require.True(t, available)
	require.False(t, ready)

	require.NoError(t, os.WriteFile(usagePath, []byte("2000000000\n"), 0o600))
	percent, available, ready := reader.sample(now.Add(5 * time.Second))
	require.True(t, available)
	require.True(t, ready)
	assert.InDelta(t, 20, percent, 0.001)
}

func TestContainerCPUReaderMissingUsage(t *testing.T) {
	reader := &containerCPUReader{fallbackCPUs: 2}
	_, available, ready := reader.sample(time.Now())
	assert.False(t, available)
	assert.False(t, ready)
}
