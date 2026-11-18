//go:build windows

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	createNoWindow  = 0x08000000
	detachedProcess = 0x00000008

	mbOK            = 0x00000000
	mbYesNo         = 0x00000004
	mbIconError     = 0x00000010
	mbIconQuestion  = 0x00000020
	mbSetForeground = 0x00010000
	idYes           = 6
)

// processRunning reports whether any process with one of these executable names exists (case-insensitive).
func processRunning(names ...string) bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if windows.Process32First(snap, &pe) != nil {
		return false
	}
	for {
		exe := windows.UTF16ToString(pe.ExeFile[:])
		for _, n := range names {
			if strings.EqualFold(exe, n) {
				return true
			}
		}
		if windows.Process32Next(snap, &pe) != nil {
			return false
		}
	}
}

// killProcesses force-closes every process with these executable names (and their children).
func killProcesses(names ...string) {
	for _, n := range names {
		cmd := exec.Command("taskkill", "/F", "/T", "/IM", n)
		hideWindow(cmd)
		_ = cmd.Run()
	}
}

// spawnDetached starts exe with args as a hidden, console-less process that outlives this one.
func spawnDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | detachedProcess}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

var messageBoxW = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

func messageBox(text string, flags uintptr) uintptr {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(appTitle)
	r, _, _ := messageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags|mbSetForeground)
	return r
}

// notify shows an error/information box. With DISCORDPROXY_NO_UI=1 (tests) it only prints.
func notify(text string) {
	if noUI() {
		os.Stderr.WriteString(text + "\n")
		return
	}
	messageBox(text, mbOK|mbIconError)
}

// confirm asks a yes/no question. With DISCORDPROXY_NO_UI=1 (tests) the answer is yes.
func confirm(text string) bool {
	if noUI() {
		return true
	}
	return messageBox(text, mbYesNo|mbIconQuestion) == idYes
}
