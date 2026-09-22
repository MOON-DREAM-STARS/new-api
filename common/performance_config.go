package common

import "sync/atomic"

// PerformanceMonitorConfig 性能监控配置
type PerformanceMonitorConfig struct {
	Enabled         bool
	CPUThreshold    int
	MemoryThreshold int
	DiskThreshold   int
}

type performanceMonitorConfigState struct {
	config     PerformanceMonitorConfig
	generation uint64
}

var performanceMonitorConfig atomic.Value

func init() {
	// 初始化默认配置
	performanceMonitorConfig.Store(performanceMonitorConfigState{
		config: PerformanceMonitorConfig{
			Enabled:         true,
			CPUThreshold:    90,
			MemoryThreshold: 90,
			DiskThreshold:   90,
		},
		generation: 1,
	})
}

// GetPerformanceMonitorConfig 获取性能监控配置
func GetPerformanceMonitorConfig() PerformanceMonitorConfig {
	return performanceMonitorConfig.Load().(performanceMonitorConfigState).config
}

// GetPerformanceMonitorConfigGeneration 获取性能监控配置代次
func GetPerformanceMonitorConfigGeneration() uint64 {
	return performanceMonitorConfig.Load().(performanceMonitorConfigState).generation
}

// SetPerformanceMonitorConfig 设置性能监控配置
func SetPerformanceMonitorConfig(config PerformanceMonitorConfig) {
	for {
		previous := performanceMonitorConfig.Load().(performanceMonitorConfigState)
		next := performanceMonitorConfigState{
			config:     config,
			generation: previous.generation + 1,
		}
		if performanceMonitorConfig.CompareAndSwap(previous, next) {
			return
		}
	}
}
