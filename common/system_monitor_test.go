package common

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCPUOverloadTrackerRequiresConsecutiveSamples(t *testing.T) {
	var tracker cpuOverloadTracker

	assert.False(t, tracker.update(true))
	assert.False(t, tracker.update(true))
	assert.True(t, tracker.update(true))

	assert.False(t, tracker.update(false))
	assert.False(t, tracker.update(true))
	assert.False(t, tracker.update(true))
	assert.True(t, tracker.update(true))
}

func TestSystemStatusFreshnessAndGeneration(t *testing.T) {
	oldConfig := GetPerformanceMonitorConfig()
	oldStatus := GetSystemStatus()
	t.Cleanup(func() {
		SetPerformanceMonitorConfig(oldConfig)
		SetSystemStatus(oldStatus)
	})

	config := oldConfig
	config.CPUThreshold = 80
	SetPerformanceMonitorConfig(config)
	generation := GetPerformanceMonitorConfigGeneration()

	current := SystemStatus{SampledAt: time.Now(), ConfigGeneration: generation}
	assert.True(t, IsSystemStatusFresh(current))
	assert.True(t, IsSystemStatusCurrent(current))

	stale := current
	stale.SampledAt = time.Now().Add(-16 * time.Second)
	assert.False(t, IsSystemStatusFresh(stale))

	SetPerformanceMonitorConfig(config)
	assert.False(t, IsSystemStatusCurrent(current))
}

func TestUpdateSystemStatusRequiresThreeContainerCPUSamples(t *testing.T) {
	dir := t.TempDir()
	usagePath := filepath.Join(dir, "cpu.stat")
	quotaPath := filepath.Join(dir, "cpu.max")
	require.NoError(t, os.WriteFile(usagePath, []byte("usage_usec 1000000\n"), 0o600))
	require.NoError(t, os.WriteFile(quotaPath, []byte("200000 100000\n"), 0o600))

	state := &systemMonitorState{
		containerCPU: &containerCPUReader{
			paths:        cgroupCPUPaths{version: 2, usagePath: usagePath, quotaPath: quotaPath},
			fallbackCPUs: 2,
		},
	}
	config := PerformanceMonitorConfig{Enabled: true, CPUThreshold: 10, MemoryThreshold: 0, DiskThreshold: 0}
	now := time.Unix(100, 0)

	status := updateSystemStatus(state, config, 1, now, true)
	assert.False(t, status.CPUOverloaded)

	for i := 1; i <= 2; i++ {
		usage := 1000000 + i*2000000
		require.NoError(t, os.WriteFile(usagePath, []byte("usage_usec "+strconv.Itoa(usage)+"\n"), 0o600))
		status = updateSystemStatus(state, config, 1, now.Add(time.Duration(i)*5*time.Second), true)
		assert.False(t, status.CPUOverloaded)
	}

	require.NoError(t, os.WriteFile(usagePath, []byte("usage_usec 7000000\n"), 0o600))
	status = updateSystemStatus(state, config, 2, now.Add(15*time.Second), true)
	assert.False(t, status.CPUOverloaded, "配置代次变化后连续计数必须重置")

	require.NoError(t, os.WriteFile(usagePath, []byte("usage_usec 9000000\n"), 0o600))
	status = updateSystemStatus(state, config, 2, now.Add(20*time.Second), true)
	assert.False(t, status.CPUOverloaded)

	require.NoError(t, os.WriteFile(usagePath, []byte("usage_usec 11000000\n"), 0o600))
	status = updateSystemStatus(state, config, 2, now.Add(25*time.Second), true)
	assert.True(t, status.CPUOverloaded)
}
