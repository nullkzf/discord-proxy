//go:build !windows && !darwin

package main

// Stand-ins so the launcher logic can be built and unit-tested on macOS/Linux. The real thing is Windows-only.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func processRunning(names ...string) bool {
	for _, n := range names {
		if exec.Command("pgrep", "-x", strings.TrimSuffix(n, ".exe")).Run() == nil {
			return true
		}
	}
	return false
}

func killProcesses(names ...string) {
	for _, n := range names {
		_ = exec.Command("pkill", "-x", strings.TrimSuffix(n, ".exe")).Run()
	}
}

func spawnDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func hideWindow(*exec.Cmd) {}

func notify(text string) { fmt.Fprintln(os.Stderr, text) }

func confirm(text string) bool {
	fmt.Fprintln(os.Stderr, text, "(assuming yes)")
	return true
}
