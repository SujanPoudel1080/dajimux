//go:build !linux && !darwin

package sysstats

import (
	"runtime"
)

type otherCollector struct{}

func newPlatformCollector() platformCollector {
	return &otherCollector{}
}

func (c *otherCollector) collect() Stats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return Stats{
		CPUPercent:    0.0,
		RAMUsedBytes:  m.Alloc,
		RAMTotalBytes: m.Sys,
	}
}
