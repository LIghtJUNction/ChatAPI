//go:build !linux

package monitoring

type hostSample struct {
	totalCPU        uint64
	idleCPU         uint64
	memoryTotal     uint64
	memoryAvailable uint64
	swapTotal       uint64
	swapFree        uint64
}

func readHostSample() hostSample { return hostSample{} }
