package completion_test

import (
	"strings"
	"testing"

	"github.com/SujanPoudel1080/dajimux/internal/completion"
)

func TestGenerate_Bash(t *testing.T) {
	script, err := completion.Generate("bash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "complete -F _dajimux dajimux") {
		t.Errorf("bash completion missing complete line")
	}
	if !strings.Contains(script, "_dajimux_sessions") {
		t.Errorf("bash completion missing session completion helper")
	}
}

func TestGenerate_Zsh(t *testing.T) {
	script, err := completion.Generate("zsh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "#compdef dajimux") {
		t.Errorf("zsh completion missing #compdef header")
	}
	if !strings.Contains(script, "_dajimux_sessions") {
		t.Errorf("zsh completion missing session completion helper")
	}
}

func TestGenerate_Fish(t *testing.T) {
	script, err := completion.Generate("fish")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "complete -f -c dajimux") {
		t.Errorf("fish completion missing complete directives")
	}
	if !strings.Contains(script, "__fish_dajimux_sessions") {
		t.Errorf("fish completion missing fish sessions helper")
	}
}

func TestGenerate_CaseInsensitive(t *testing.T) {
	for _, shell := range []string{"Bash", " ZSH ", "Fish"} {
		script, err := completion.Generate(shell)
		if err != nil {
			t.Errorf("expected %q to succeed, got error: %v", shell, err)
		}
		if len(script) == 0 {
			t.Errorf("expected non-empty script for %q", shell)
		}
	}
}

func TestGenerate_Unsupported(t *testing.T) {
	_, err := completion.Generate("powershell")
	if err == nil {
		t.Fatalf("expected error for unsupported shell, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("expected error message to mention unsupported shell, got: %v", err)
	}
}
