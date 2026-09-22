package middleware

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemPerformanceCheckUsesFreshCurrentStatus(t *testing.T) {
	oldConfig := common.GetPerformanceMonitorConfig()
	oldStatus := common.GetSystemStatus()
	t.Cleanup(func() {
		common.SetPerformanceMonitorConfig(oldConfig)
		common.SetSystemStatus(oldStatus)
	})

	config := common.PerformanceMonitorConfig{
		Enabled:         true,
		CPUThreshold:    90,
		MemoryThreshold: 90,
		DiskThreshold:   90,
	}
	common.SetPerformanceMonitorConfig(config)
	generation := common.GetPerformanceMonitorConfigGeneration()

	common.SetSystemStatus(common.SystemStatus{
		CPUUsage:         99.5,
		CPUOverloaded:    true,
		SampledAt:        time.Now(),
		ConfigGeneration: generation,
	})
	err := checkSystemPerformance()
	require.Error(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	assert.Equal(t, "system_cpu_overloaded", string(err.GetErrorCode()))

	common.SetSystemStatus(common.SystemStatus{
		CPUUsage:         99.5,
		CPUOverloaded:    false,
		SampledAt:        time.Now(),
		ConfigGeneration: generation,
	})
	assert.Nil(t, checkSystemPerformance(), "单次高采样尚未达到连续确认时不得熔断")

	common.SetSystemStatus(common.SystemStatus{
		CPUUsage:         99.5,
		CPUOverloaded:    true,
		SampledAt:        time.Now().Add(-time.Minute),
		ConfigGeneration: generation,
	})
	assert.Nil(t, checkSystemPerformance())

	common.SetSystemStatus(common.SystemStatus{
		CPUUsage:         99.5,
		CPUOverloaded:    true,
		SampledAt:        time.Now(),
		ConfigGeneration: generation,
	})
	common.SetPerformanceMonitorConfig(config)
	assert.Nil(t, checkSystemPerformance())

	common.SetPerformanceMonitorConfig(config)
	common.SetSystemStatus(common.SystemStatus{
		CPUUsage:         10,
		SampledAt:        time.Now(),
		ConfigGeneration: common.GetPerformanceMonitorConfigGeneration(),
	})
	assert.Nil(t, checkSystemPerformance())
}
