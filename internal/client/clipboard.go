package client

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
)

// copyToClipboard synchronizes text with the clipboard using OSC 52,
// with fallback to system clipboard utilities (wl-copy, xclip, xsel, pbcopy).
func copyToClipboard(text string) {
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	fmt.Printf("\033]52;c;%s\a", b64)

	go func() {
		// Wayland
		if path, err := exec.LookPath("wl-copy"); err == nil {
			cmd := exec.Command(path)
			cmd.Stdin = strings.NewReader(text)
			_ = cmd.Run()
			return
		}
		// X11 xclip
		if path, err := exec.LookPath("xclip"); err == nil {
			cmd := exec.Command(path, "-selection", "clipboard")
			cmd.Stdin = strings.NewReader(text)
			_ = cmd.Run()
			return
		}
		// X11 xsel
		if path, err := exec.LookPath("xsel"); err == nil {
			cmd := exec.Command(path, "--clipboard", "--input")
			cmd.Stdin = strings.NewReader(text)
			_ = cmd.Run()
			return
		}
		// macOS pbcopy
		if path, err := exec.LookPath("pbcopy"); err == nil {
			cmd := exec.Command(path)
			cmd.Stdin = strings.NewReader(text)
			_ = cmd.Run()
			return
		}
	}()
}
