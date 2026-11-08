package man_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/SujanPoudel1080/dajimux/internal/man"
)

func TestGenerateManPage(t *testing.T) {
	page := man.GenerateManPage("0.1.5")

	expectedSections := []string{
		".TH DAJIMUX 1",
		".SH NAME",
		".SH SYNOPSIS",
		".SH DESCRIPTION",
		".SH COMMANDS",
		".SH VIM MODAL CONTROLS",
		".SH MOUSE SUPPORT",
		".SH CONFIGURATION",
		".SH MODEL CONTEXT PROTOCOL (MCP)",
		".SH FILES",
		".SH AUTHORS",
		"Sujan Poudel <sujanpoudel1080@gmail.com>",
	}

	for _, section := range expectedSections {
		if !strings.Contains(page, section) {
			t.Errorf("expected man page to contain %q", section)
		}
	}
}

func TestView_Raw(t *testing.T) {
	var buf bytes.Buffer
	err := man.View("0.1.5", true, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, ".TH DAJIMUX 1") {
		t.Errorf("raw view output missing .TH header")
	}
}
