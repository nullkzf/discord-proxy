//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// processRunning reports whether a process with exactly one of these names is running (pgrep -x).
// Discord's helper processes are named "Discord Helper …", so only the main app matches "Discord".
func processRunning(names ...string) bool {
	for _, n := range names {
		if exec.Command("/usr/bin/pgrep", "-x", n).Run() == nil {
			return true
		}
	}
	return false
}

// killProcesses asks the processes to quit (SIGTERM — Electron quits cleanly on it), then forces the stragglers.
// It matches the exact process name only, never an application name, so nothing else can be hit.
func killProcesses(names ...string) {
	for _, n := range names {
		_ = exec.Command("/usr/bin/pkill", "-TERM", "-x", n).Run()
	}
	for i := 0; i < 15 && processRunning(names...); i++ {
		time.Sleep(200 * time.Millisecond)
	}
	for _, n := range names {
		_ = exec.Command("/usr/bin/pkill", "-KILL", "-x", n).Run()
	}
}

// spawnDetached starts exe with args in its own session so it outlives this process.
func spawnDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func hideWindow(*exec.Cmd) {}

// dialog shows a native macOS dialog (text is passed as an argument, so no quoting problems).
func dialog(text string, buttons string, def string, icon string) bool {
	script := fmt.Sprintf(`on run argv
display dialog (item 1 of argv) with title "Discord Proxy" buttons %s default button %q with icon %s
end run`, buttons, def, icon)
	return exec.Command("/usr/bin/osascript", "-e", script, text).Run() == nil
}

// notify shows an error/information box. With DISCORDPROXY_NO_UI=1 (tests) it only prints.
func notify(text string) {
	if noUI() {
		fmt.Fprintln(os.Stderr, text)
		return
	}
	dialog(text, `{"OK"}`, "OK", "caution")
}

// confirm asks a yes/no question. With DISCORDPROXY_NO_UI=1 (tests) the answer is yes.
func confirm(text string) bool {
	if noUI() {
		return true
	}
	return dialog(text, `{"Cancel", "Restart Discord"}`, "Restart Discord", "note")
}
