package sysstats

import (
	"fmt"
	"strings"
)

// Stats holds snapshot telemetry for system CPU and RAM utilization.
type Stats struct {
	CPUPercent    float64 // 0.0 to 100.0%
	RAMUsedBytes  uint64  // Used RAM in bytes
	RAMTotalBytes uint64  // Total RAM in bytes
}

// RAMPercent calculates the percentage of utilized system RAM.
func (s Stats) RAMPercent() float64 {
	if s.RAMTotalBytes == 0 {
		return 0.0
	}
	pct := (float64(s.RAMUsedBytes) / float64(s.RAMTotalBytes)) * 100.0
	if pct < 0.0 {
		return 0.0
	}
	if pct > 100.0 {
		return 100.0
	}
	return pct
}

// Format returns a clean plain-text summary suitable for terminal status lines.
// Example: "CPU 12% | RAM 4.2G/16.0G"
func (s Stats) Format() string {
	var b strings.Builder
	cpu := s.CPUPercent
	if cpu < 0 {
		cpu = 0
	}
	b.WriteString(fmt.Sprintf("CPU %.0f%%", cpu))

	if s.RAMTotalBytes > 0 {
		b.WriteString(fmt.Sprintf(" | RAM %s/%s", formatBytes(s.RAMUsedBytes), formatBytes(s.RAMTotalBytes)))
	} else if s.RAMUsedBytes > 0 {
		b.WriteString(fmt.Sprintf(" | RAM %s", formatBytes(s.RAMUsedBytes)))
	}

	return b.String()
}

// FormatCompact returns a shorter version for constrained terminals.
// Example: "CPU 12% | RAM 45%"
func (s Stats) FormatCompact() string {
	cpu := s.CPUPercent
	if cpu < 0 {
		cpu = 0
	}
	if s.RAMTotalBytes > 0 {
		return fmt.Sprintf("CPU %.0f%% | RAM %.0f%%", cpu, s.RAMPercent())
	}
	return fmt.Sprintf("CPU %.0f%%", cpu)
}

// FormatANSI returns ANSI color-coded text reflecting utilization thresholds.
func (s Stats) FormatANSI(baseBg, baseFg string) string {
	cpu := s.CPUPercent
	if cpu < 0 {
		cpu = 0
	}

	// CPU color
	cpuColor := "\033[38;5;82m" // Green
	if cpu >= 85.0 {
		cpuColor = "\033[1m\033[38;5;196m" // Bold Bright Red
	} else if cpu >= 70.0 {
		cpuColor = "\033[38;5;214m" // Orange/Yellow
	}

	// RAM color
	ramPct := s.RAMPercent()
	ramColor := "\033[38;5;81m" // Cyan
	if ramPct >= 90.0 {
		ramColor = "\033[1m\033[38;5;196m" // Bold Bright Red
	} else if ramPct >= 75.0 {
		ramColor = "\033[38;5;214m" // Orange/Yellow
	}

	dimFg := "\033[38;5;242m"
	reset := "\033[0m" + baseBg + baseFg

	if s.RAMTotalBytes > 0 {
		return fmt.Sprintf("%sCPU %.0f%%%s%s | %s%sRAM %s/%s%s",
			cpuColor, cpu, reset,
			dimFg, reset,
			ramColor, formatBytes(s.RAMUsedBytes), formatBytes(s.RAMTotalBytes), reset)
	}

	return fmt.Sprintf("%sCPU %.0f%%%s", cpuColor, cpu, reset)
}

func formatBytes(bytes uint64) string {
	const gb = 1024 * 1024 * 1024
	const mb = 1024 * 1024
	if bytes >= gb {
		return fmt.Sprintf("%.1fG", float64(bytes)/float64(gb))
	}
	return fmt.Sprintf("%dM", bytes/mb)
}
