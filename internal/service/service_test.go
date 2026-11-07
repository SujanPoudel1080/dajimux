package service

import (
	"runtime"
	"strings"
	"testing"
)

func TestNewServiceManager(t *testing.T) {
	mgr, err := NewServiceManager()
	if err != nil {
		t.Fatalf("NewServiceManager error: %v", err)
	}
	if mgr.BinaryPath == "" {
		t.Errorf("expected non-empty binary path")
	}
}

func TestServiceManagerPaths(t *testing.T) {
	mgr := &ServiceManager{BinaryPath: "/usr/local/bin/dajimux"}

	if runtime.GOOS == "darwin" {
		plist, err := mgr.darwinPlistPath()
		if err != nil {
			t.Fatalf("darwinPlistPath error: %v", err)
		}
		if !strings.HasSuffix(plist, "Library/LaunchAgents/com.sujanpoudel.dajimux.plist") {
			t.Errorf("unexpected plist path: %s", plist)
		}
	} else if runtime.GOOS == "linux" {
		unit, err := mgr.linuxUnitPath()
		if err != nil {
			t.Fatalf("linuxUnitPath error: %v", err)
		}
		if !strings.HasSuffix(unit, ".config/systemd/user/dajimux.service") {
			t.Errorf("unexpected unit path: %s", unit)
		}
	}
}
