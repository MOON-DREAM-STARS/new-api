package common

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

const (
	systemMonitorInterval         = 5 * time.Second
	systemStatusMaxAge            = 15 * time.Second
	cpuOverloadConsecutiveSamples = 3
)

// DiskSpaceInfo 磁盘空间信息
type DiskSpaceInfo struct {
	// 总空间（字节）
	Total uint64 `json:"total"`
	// 可用空间（字节）
	Free uint64 `json:"free"`
	// 已使用空间（字节）
	Used uint64 `json:"used"`
	// 使用百分比
	UsedPercent float64 `json:"used_percent"`
}

// SystemStatus 系统状态信息
type SystemStatus struct {
	CPUUsage         float64
	MemoryUsage      float64
	DiskUsage        float64
	CPUOverloaded    bool
	MemoryOverloaded bool
	DiskOverloaded   bool
	SampledAt        time.Time
	ConfigGeneration uint64
}

type cpuOverloadTracker struct {
	consecutive int
	overloaded  bool
}

func (t *cpuOverloadTracker) update(over bool) bool {
	if !over {
		t.consecutive = 0
		t.overloaded = false
		return false
	}
	t.consecutive++
	if t.consecutive >= cpuOverloadConsecutiveSamples {
		t.overloaded = true
	}
	return t.overloaded
}

type systemMonitorState struct {
	containerCPU                     *containerCPUReader
	cpuOverload                      cpuOverloadTracker
	configGeneration                 uint64
	lastCPUOverloaded                bool
	containerCPUCapabilityLoggedDown bool
}

var latestSystemStatus atomic.Value

func init() {
	latestSystemStatus.Store(SystemStatus{})
}

func newSystemMonitorState() *systemMonitorState {
	return &systemMonitorState{containerCPU: newContainerCPUReader()}
}

// StartSystemMonitor 启动系统监控
func StartSystemMonitor() {
	go func() {
		state := newSystemMonitorState()
		for {
			config := GetPerformanceMonitorConfig()
			generation := GetPerformanceMonitorConfigGeneration()
			if !config.Enabled {
				time.Sleep(30 * time.Second)
				continue
			}

			status := updateSystemStatus(state, config, generation, time.Now(), IsRunningInContainer())
			logCPUOverloadTransition(state, status, config.CPUThreshold)
			latestSystemStatus.Store(status)
			time.Sleep(systemMonitorInterval)
		}
	}()
}

func updateSystemStatus(state *systemMonitorState, config PerformanceMonitorConfig, generation uint64, now time.Time, inContainer bool) SystemStatus {
	if state.configGeneration != generation {
		state.configGeneration = generation
		state.cpuOverload = cpuOverloadTracker{}
	}

	var status SystemStatus
	status.SampledAt = now
	status.ConfigGeneration = generation

	cpuUsage, cpuMetricAvailable, cpuReady := 0.0, false, false
	if inContainer {
		cpuUsage, cpuMetricAvailable, cpuReady = state.containerCPU.sample(now)
		if !cpuMetricAvailable && !state.containerCPUCapabilityLoggedDown {
			SysLog("system CPU monitor: container CPU metrics unavailable; CPU overload guard disabled")
			state.containerCPUCapabilityLoggedDown = true
		}
	} else {
		percents, err := cpu.Percent(0, false)
		if err == nil && len(percents) > 0 {
			cpuUsage = percents[0]
			cpuMetricAvailable = true
			cpuReady = true
		}
	}

	if cpuReady {
		status.CPUUsage = cpuUsage
	}

	memInfo, err := mem.VirtualMemory()
	if err == nil {
		status.MemoryUsage = memInfo.UsedPercent
	}

	diskInfo := GetDiskSpaceInfo()
	if diskInfo.Total > 0 {
		status.DiskUsage = diskInfo.UsedPercent
	}

	cpuOver := cpuReady && config.CPUThreshold > 0 && int(status.CPUUsage) > config.CPUThreshold
	memoryOver := config.MemoryThreshold > 0 && int(status.MemoryUsage) > config.MemoryThreshold
	diskOver := config.DiskThreshold > 0 && int(status.DiskUsage) > config.DiskThreshold
	status.CPUOverloaded = state.cpuOverload.update(cpuOver)
	status.MemoryOverloaded = memoryOver
	status.DiskOverloaded = diskOver
	return status
}

func logCPUOverloadTransition(state *systemMonitorState, status SystemStatus, threshold int) {
	if status.CPUOverloaded == state.lastCPUOverloaded {
		return
	}
	if status.CPUOverloaded {
		SysLog(fmt.Sprintf("system cpu overload detected: current=%.1f%%, threshold=%d%%", status.CPUUsage, threshold))
	} else {
		SysLog(fmt.Sprintf("system cpu overload cleared: current=%.1f%%", status.CPUUsage))
	}
	state.lastCPUOverloaded = status.CPUOverloaded
}

// GetSystemStatus 获取当前系统状态
func GetSystemStatus() SystemStatus {
	return latestSystemStatus.Load().(SystemStatus)
}

// SetSystemStatus 覆盖当前系统状态；系统监控与测试使用。
func SetSystemStatus(status SystemStatus) {
	latestSystemStatus.Store(status)
}

// IsSystemStatusFresh 判断采样是否足够新，过期状态不参与过载熔断。
func IsSystemStatusFresh(status SystemStatus) bool {
	return !status.SampledAt.IsZero() && time.Since(status.SampledAt) <= systemStatusMaxAge
}

// IsSystemStatusCurrent 判断采样是否属于当前性能监控配置代次。
func IsSystemStatusCurrent(status SystemStatus) bool {
	return status.ConfigGeneration == GetPerformanceMonitorConfigGeneration()
}
