package sysstats

import (
	"strings"
	"testing"
	"time"
)

func TestStatsFormatting(t *testing.T) {
	s := Stats{
		CPUPercent:    12.4,
		RAMUsedBytes:  4 * 1024 * 1024 * 1024,  // 4GB
		RAMTotalBytes: 16 * 1024 * 1024 * 1024, // 16GB
	}

	// RAMPercent
	pct := s.RAMPercent()
	if pct < 24.9 || pct > 25.1 {
		t.Errorf("expected ~25%% RAM percent, got %.2f", pct)
	}

	// Format
	fmtStr := s.Format()
	if !strings.Contains(fmtStr, "CPU 12%") || !strings.Contains(fmtStr, "RAM 4.0G/16.0G") {
		t.Errorf("unexpected Format output: %q", fmtStr)
	}

	// FormatCompact
	compactStr := s.FormatCompact()
	if !strings.Contains(compactStr, "CPU 12%") || !strings.Contains(compactStr, "RAM 25%") {
		t.Errorf("unexpected FormatCompact output: %q", compactStr)
	}

	// FormatANSI
	ansiStr := s.FormatANSI("\033[48;5;236m", "\033[38;5;255m")
	if !strings.Contains(ansiStr, "CPU 12%") || !strings.Contains(ansiStr, "RAM 4.0G/16.0G") {
		t.Errorf("unexpected FormatANSI output: %q", ansiStr)
	}

	// High CPU ANSI styling
	highCPU := Stats{CPUPercent: 92.0, RAMUsedBytes: 1024, RAMTotalBytes: 2048}
	highAnsi := highCPU.FormatANSI("", "")
	if !strings.Contains(highAnsi, "\033[38;5;196m") {
		t.Errorf("expected red color code for high CPU in %q", highAnsi)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		bytes    uint64
		expected string
	}{
		{500 * 1024 * 1024, "500M"},
		{1024 * 1024 * 1024, "1.0G"},
		{8 * 1024 * 1024 * 1024, "8.0G"},
		{16 * 1024 * 1024 * 1024, "16.0G"},
	}

	for _, c := range cases {
		out := formatBytes(c.bytes)
		if out != c.expected {
			t.Errorf("formatBytes(%d) = %q, expected %q", c.bytes, out, c.expected)
		}
	}
}

func TestCollectorLifecycle(t *testing.T) {
	c := NewCollector(50 * time.Millisecond)
	st := c.Stats()
	// Stats should have been initialized on creation
	t.Logf("Initial collector stats: CPU=%.1f%%, RAM=%s/%s",
		st.CPUPercent, formatBytes(st.RAMUsedBytes), formatBytes(st.RAMTotalBytes))

	updated := make(chan Stats, 5)
	c.Start(func(s Stats) {
		select {
		case updated <- s:
		default:
		}
	})

	select {
	case s := <-updated:
		t.Logf("Received update: CPU=%.1f%%, RAM=%s/%s",
			s.CPUPercent, formatBytes(s.RAMUsedBytes), formatBytes(s.RAMTotalBytes))
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for collector update")
	}

	c.Stop()
}
