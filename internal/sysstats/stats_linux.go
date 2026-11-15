//go:build linux

package sysstats

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
)

type linuxCollector struct {
	mu        sync.Mutex
	prevTotal uint64
	prevIdle  uint64
}

func newPlatformCollector() platformCollector {
	return &linuxCollector{}
}

func (c *linuxCollector) collect() Stats {
	st := Stats{}
	memUsed, memTotal := readLinuxMemory()
	st.RAMUsedBytes = memUsed
	st.RAMTotalBytes = memTotal

	c.mu.Lock()
	cpuPct := c.readLinuxCPU()
	c.mu.Unlock()
	st.CPUPercent = cpuPct

	return st
}

func readLinuxMemory() (used, total uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var memTotal, memFree, memAvailable, buffers, cached uint64
	hasAvailable := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(parts[1], 10, 64)
		// /proc/meminfo numbers are in kilobytes (kB)
		bytesVal := val * 1024

		switch parts[0] {
		case "MemTotal:":
			memTotal = bytesVal
		case "MemFree:":
			memFree = bytesVal
		case "MemAvailable:":
			memAvailable = bytesVal
			hasAvailable = true
		case "Buffers:":
			buffers = bytesVal
		case "Cached:":
			cached = bytesVal
		}
	}

	if memTotal == 0 {
		return 0, 0
	}

	if hasAvailable {
		if memTotal >= memAvailable {
			used = memTotal - memAvailable
		}
	} else {
		free := memFree + buffers + cached
		if memTotal >= free {
			used = memTotal - free
		}
	}

	return used, memTotal
}

func (c *linuxCollector) readLinuxCPU() float64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0.0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0.0
	}
	line := scanner.Text()
	// Line format: cpu  user nice system idle iowait irq softirq steal guest guest_nice
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0.0
	}

	var total uint64
	var idleAll uint64

	for i := 1; i < len(fields); i++ {
		val, _ := strconv.ParseUint(fields[i], 10, 64)
		total += val
		if i == 4 || i == 5 { // idle (4), iowait (5)
			idleAll += val
		}
	}

	if c.prevTotal == 0 {
		// First reading: store baseline
		c.prevTotal = total
		c.prevIdle = idleAll
		return 0.0
	}

	deltaTotal := total - c.prevTotal
	deltaIdle := idleAll - c.prevIdle
	c.prevTotal = total
	c.prevIdle = idleAll

	if deltaTotal == 0 {
		return 0.0
	}

	used := float64(deltaTotal - deltaIdle)
	pct := (used / float64(deltaTotal)) * 100.0
	if pct < 0.0 {
		pct = 0.0
	}
	if pct > 100.0 {
		pct = 100.0
	}
	return pct
}
