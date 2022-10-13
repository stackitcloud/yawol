// Package hostmetrics is used in yawollet to get metrics for the host of loadbalancer
package hostmetrics

import (
	"fmt"
	"runtime"
	"strconv"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 2, 64)
}

func formatUint(u uint64) string {
	return strconv.FormatUint(u, 10)
}

func GetLoad() (load1, load5, load15 string, err error) {
	avg, err := load.Avg()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to fetch load information from host: %w", err)
	}
	return formatFloat(avg.Load1), formatFloat(avg.Load5), formatFloat(avg.Load15), nil
}

// GetCPUNum returns number of CPUs for node
func GetCPUNum() int {
	return runtime.NumCPU()
}

// GetCPUStealTime returns aggregated cpu steal time of all cpus for node
func GetCPUStealTime() (string, error) {
	const perCPU bool = false
	stats, err := cpu.Times(perCPU)
	if err != nil {
		return "", fmt.Errorf("failed to return steal time")
	}
	if len(stats) != 1 {
		return "", fmt.Errorf("expected exactly one CPU stats element, got %d", len(stats))
	}
	return formatFloat(stats[0].Steal), nil
}

func GetMem() (
	totalMemory string,
	freeMemory string,
	availableMemory string,
	err error,
) {
	stats, err := mem.VirtualMemory()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get virtual memory stats: %w", err)
	}
	return formatUint(stats.Total), formatUint(stats.Free), formatUint(stats.Available), nil
}
