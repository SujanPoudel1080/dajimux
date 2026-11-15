//go:build darwin

package sysstats

import (
	"bufio"
	"encoding/binary"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type darwinCollector struct {
	mu           sync.Mutex
	cachedTotal  uint64
	pageSize     uint64
	prevCPU      float64
	sampleCount  int
}

func newPlatformCollector() platformCollector {
	return &darwinCollector{
		pageSize: 4096, // default page size
	}
}

func (c *darwinCollector) collect() Stats {
	st := Stats{}

	// 1. Total RAM (queried once via sysctl and cached)
	if c.cachedTotal == 0 {
		c.cachedTotal = getDarwinTotalMemory()
	}
	st.RAMTotalBytes = c.cachedTotal

	// 2. Used RAM via vm_stat
	st.RAMUsedBytes = getDarwinUsedMemory(st.RAMTotalBytes)

	// 3. CPU Utilization
	st.CPUPercent = c.getDarwinCPU()

	return st
}

func getDarwinTotalMemory() uint64 {
	// Try native sysctl("hw.memsize") first
	if val, err := syscall.Sysctl("hw.memsize"); err == nil && len(val) >= 4 {
		buf := make([]byte, 8)
		copy(buf, []byte(val))
		total := binary.LittleEndian.Uint64(buf)
		if total > 0 {
			return total
		}
	}

	// Fallback to sysctl command
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err == nil {
		if total, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); err == nil {
			return total
		}
	}

	return 0
}

func getDarwinUsedMemory(totalMem uint64) uint64 {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}

	var pageSize uint64 = 4096
	var active, wired, compressor, free, speculative uint64

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Mach Virtual Memory Statistics:") {
			if idx := strings.Index(line, "page size of "); idx != -1 {
				sub := line[idx+len("page size of "):]
				if endIdx := strings.Index(sub, " bytes"); endIdx != -1 {
					if sz, err := strconv.ParseUint(strings.TrimSpace(sub[:endIdx]), 10, 64); err == nil && sz > 0 {
						pageSize = sz
					}
				}
			}
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSuffix(strings.TrimSpace(parts[1]), ".")
		val, _ := strconv.ParseUint(valStr, 10, 64)

		switch key {
		case "Pages active":
			active = val
		case "Pages wired down":
			wired = val
		case "Pages occupied by compressor":
			compressor = val
		case "Pages free":
			free = val
		case "Pages speculative":
			speculative = val
		}
	}

	// Used memory in macOS is typically Active + Wired + Compressor
	used := (active + wired + compressor) * pageSize
	if used > 0 && totalMem > 0 && used <= totalMem {
		return used
	}

	// Alternative calculation: total - (free + speculative) * pageSize
	if totalMem > 0 {
		available := (free + speculative) * pageSize
		if totalMem > available {
			return totalMem - available
		}
	}

	return used
}

func (c *darwinCollector) getDarwinCPU() float64 {
	// 1. Try ps -A -o %cpu for active aggregate process utilization
	out, err := exec.Command("ps", "-A", "-o", "%cpu").Output()
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		var sum float64
		for scanner.Scan() {
			txt := strings.TrimSpace(scanner.Text())
			if txt == "%CPU" || txt == "" {
				continue
			}
			if val, err := strconv.ParseFloat(txt, 64); err == nil {
				sum += val
			}
		}
		numCPU := float64(runtime.NumCPU())
		if numCPU < 1.0 {
			numCPU = 1.0
		}
		pct := sum / numCPU
		if pct < 0.0 {
			pct = 0.0
		}
		if pct > 100.0 {
			pct = 100.0
		}
		c.prevCPU = pct
		return pct
	}

	// 2. Fallback to native sysctl load average
	if loadVal, err := syscall.Sysctl("vm.loadavg"); err == nil && len(loadVal) >= 16 {
		raw := []byte(loadVal)
		ld0 := binary.LittleEndian.Uint32(raw[0:4])
		fscale := binary.LittleEndian.Uint32(raw[12:16])
		if fscale == 0 {
			fscale = 2048
		}
		load1 := float64(ld0) / float64(fscale)
		numCPU := float64(runtime.NumCPU())
		if numCPU < 1.0 {
			numCPU = 1.0
		}
		pct := (load1 / numCPU) * 100.0
		if pct > 100.0 {
			pct = 100.0
		}
		return pct
	}

	return c.prevCPU
}
