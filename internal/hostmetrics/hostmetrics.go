// Package hostmetrics is used in yawollet to get metrics for the host of loadbalancer
package hostmetrics

import (
	"io/ioutil"
	"runtime"
	"strings"
)

func GetLoad() (load1 string, load5 string, load15 string, err error) {
	loadFile, err := ioutil.ReadFile("/proc/loadavg")
	if err != nil {
		return "", "", "", err
	}
	load := strings.Split(string(loadFile), " ")
	if len(load) != 5 {
		return "", "", "", err
	}
	return load[0], load[1], load[2], nil
}

// GetCPUNum returns number of CPUs for node
func GetCPUNum() int {
	return runtime.NumCPU()
}

// GetCPUStealTime returns aggregated cpu steal time of all cpus for node
func GetCPUStealTime() (string, error) {
	statFile, err := ioutil.ReadFile("/proc/stat")
	if err != nil {
		return "", err
	}

	var stealTime string
	statFileLines := strings.Split(string(statFile), "\n")
	for _, info := range statFileLines {
		// search for line:
		// cpu  7484550 1016 873590 23819469 35883 0 131941 0 0 0
		if !strings.HasPrefix(info, "cpu ") {
			continue
		}

		infoLine := strings.TrimPrefix(info, "cpu")
		infoLine = strings.TrimSpace(infoLine)
		infoLineNumbers := strings.Split(infoLine, " ")
		if len(infoLineNumbers) < 8 {
			stealTime = "0"
		} else {
			stealTime = infoLineNumbers[7]
		}

		break
	}

	return stealTime, nil
}

func GetMem() (
	totalMemory string,
	freeMemory string,
	availableMemory string,
	err error,
) {
	memFile, err := ioutil.ReadFile("/proc/meminfo")
	if err != nil {
		return "", "", "", err
	}

	var memTotal, memFree, memAvailable string
	memInfos := strings.Split(string(memFile), "\n")
	for _, info := range memInfos {
		if strings.HasPrefix(info, "MemTotal") {
			memTotal = strings.TrimPrefix(info, "MemTotal:")
			memTotal = strings.TrimSuffix(memTotal, "kB")
			memTotal = strings.TrimSpace(memTotal)
			continue
		}

		if strings.HasPrefix(info, "MemFree") {
			memFree = strings.TrimPrefix(info, "MemFree:")
			memFree = strings.TrimSuffix(memFree, "kB")
			memFree = strings.TrimSpace(memFree)
			continue
		}

		if strings.HasPrefix(info, "MemAvailable") {
			memAvailable = strings.TrimPrefix(info, "MemAvailable:")
			memAvailable = strings.TrimSuffix(memAvailable, "kB")
			memAvailable = strings.TrimSpace(memAvailable)
			continue
		}
	}

	return memTotal, memFree, memAvailable, nil
}
