package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ServiceManager handles installing, checking, and uninstalling the background daemon service.
type ServiceManager struct {
	BinaryPath string
}

// NewServiceManager initializes the manager using the current binary executable path.
func NewServiceManager() (*ServiceManager, error) {
	bin, err := os.Executable()
	if err != nil {
		bin = "dajimux"
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		bin = "dajimux"
	}
	return &ServiceManager{BinaryPath: bin}, nil
}

// Install writes the appropriate daemon service definition and registers it with the OS.
func (s *ServiceManager) Install() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return s.installDarwin()
	case "linux":
		return s.installLinux()
	default:
		return "", fmt.Errorf("service installation is not supported on %s", runtime.GOOS)
	}
}

// Uninstall unregisters and removes the service file.
func (s *ServiceManager) Uninstall() error {
	switch runtime.GOOS {
	case "darwin":
		return s.uninstallDarwin()
	case "linux":
		return s.uninstallLinux()
	default:
		return fmt.Errorf("service uninstall is not supported on %s", runtime.GOOS)
	}
}

// Status reports the active operational state of the background service.
func (s *ServiceManager) Status() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return s.statusDarwin()
	case "linux":
		return s.statusLinux()
	default:
		return "", fmt.Errorf("service status is not supported on %s", runtime.GOOS)
	}
}

// --- macOS (launchd) implementation ---

func (s *ServiceManager) darwinPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", "com.sujanpoudel.dajimux.plist"), nil
}

func (s *ServiceManager) installDarwin() (string, error) {
	plistPath, err := s.darwinPlistPath()
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(plistPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create LaunchAgents directory: %w", err)
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.sujanpoudel.dajimux</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>daemon</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/dajimux.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/dajimux.err</string>
</dict>
</plist>
`, s.BinaryPath)

	if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return "", fmt.Errorf("write launchd plist: %w", err)
	}

	// Load service with launchctl
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	_ = exec.Command("launchctl", "load", "-w", plistPath).Run()

	return plistPath, nil
}

func (s *ServiceManager) uninstallDarwin() error {
	plistPath, err := s.darwinPlistPath()
	if err != nil {
		return err
	}

	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist file: %w", err)
	}

	return nil
}

func (s *ServiceManager) statusDarwin() (string, error) {
	plistPath, err := s.darwinPlistPath()
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return "Service not installed", nil
	}

	out, err := exec.Command("launchctl", "list", "com.sujanpoudel.dajimux").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Service installed at %s (inactive)", plistPath), nil
	}

	return fmt.Sprintf("Service running (launchd: com.sujanpoudel.dajimux)\n%s", strings.TrimSpace(string(out))), nil
}

// --- Linux (systemd) implementation ---

func (s *ServiceManager) linuxUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", "dajimux.service"), nil
}

func (s *ServiceManager) installLinux() (string, error) {
	unitPath, err := s.linuxUnitPath()
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(unitPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create systemd user directory: %w", err)
	}

	unitContent := fmt.Sprintf(`[Unit]
Description=dajimux terminal multiplexer daemon
Documentation=https://github.com/SujanPoudel1080/dajimux

[Service]
Type=simple
ExecStart=%s daemon
Restart=on-failure
RestartSec=3s

[Install]
WantedBy=default.target
`, s.BinaryPath)

	if err := os.WriteFile(unitPath, []byte(unitContent), 0644); err != nil {
		return "", fmt.Errorf("write systemd unit: %w", err)
	}

	// Reload systemd user daemon and enable service
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	_ = exec.Command("systemctl", "--user", "enable", "--now", "dajimux").Run()

	return unitPath, nil
}

func (s *ServiceManager) uninstallLinux() error {
	unitPath, err := s.linuxUnitPath()
	if err != nil {
		return err
	}

	_ = exec.Command("systemctl", "--user", "disable", "--now", "dajimux").Run()
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file: %w", err)
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

	return nil
}

func (s *ServiceManager) statusLinux() (string, error) {
	unitPath, err := s.linuxUnitPath()
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		return "Service not installed", nil
	}

	out, _ := exec.Command("systemctl", "--user", "status", "dajimux").CombinedOutput()
	return string(out), nil
}
