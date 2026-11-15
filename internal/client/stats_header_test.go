package client

import (
	"strings"
	"testing"

	"github.com/SujanPoudel1080/dajimux/internal/sysstats"
)

func TestFormatHeaderStats(t *testing.T) {
	st := sysstats.Stats{
		CPUPercent:    15.0,
		RAMUsedBytes:  4 * 1024 * 1024 * 1024,
		RAMTotalBytes: 8 * 1024 * 1024 * 1024,
	}

	// 1. Ample space -> full format
	plain, ansi := formatHeaderStats(st, 50, "", "")
	if !strings.Contains(plain, "CPU 15%") || !strings.Contains(plain, "RAM 4.0G/8.0G") {
		t.Errorf("expected full format with ample space, got %q", plain)
	}
	if ansi == "" {
		t.Errorf("expected non-empty ANSI format")
	}

	// 2. Medium space -> compact format
	plainCompact, _ := formatHeaderStats(st, 20, "", "")
	if !strings.Contains(plainCompact, "CPU 15%") || !strings.Contains(plainCompact, "RAM 50%") {
		t.Errorf("expected compact format with medium space, got %q", plainCompact)
	}

	// 3. Small space -> CPU only
	plainShort, _ := formatHeaderStats(st, 12, "", "")
	if plainShort != "CPU 15%" {
		t.Errorf("expected short format 'CPU 15%%', got %q", plainShort)
	}

	// 4. Very tight space -> nothing
	plainNone, _ := formatHeaderStats(st, 5, "", "")
	if plainNone != "" {
		t.Errorf("expected empty string for tight space, got %q", plainNone)
	}
}
