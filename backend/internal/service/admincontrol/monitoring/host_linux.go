//go:build linux

package monitoring

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type hostSample struct {
	totalCPU        uint64
	idleCPU         uint64
	memoryTotal     uint64
	memoryAvailable uint64
	swapTotal       uint64
	swapFree        uint64
}

func readHostSample() hostSample {
	var sample hostSample
	if data, err := os.ReadFile("/proc/stat"); err == nil {
		line, _, _ := strings.Cut(string(data), "\n")
		fields := strings.Fields(line)
		for index, raw := range fields[1:] {
			value, _ := strconv.ParseUint(raw, 10, 64)
			sample.totalCPU += value
			if index == 3 || index == 4 {
				sample.idleCPU += value
			}
		}
	}
	if file, err := os.Open("/proc/meminfo"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 2 {
				continue
			}
			value, _ := strconv.ParseUint(fields[1], 10, 64)
			value *= 1024
			switch strings.TrimSuffix(fields[0], ":") {
			case "MemTotal":
				sample.memoryTotal = value
			case "MemAvailable":
				sample.memoryAvailable = value
			case "SwapTotal":
				sample.swapTotal = value
			case "SwapFree":
				sample.swapFree = value
			}
		}
	}
	return sample
}
