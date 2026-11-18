package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// macOS: Discord lives in a normal app bundle; it is started through LaunchServices (`open`), which keeps
// Discord its own "responsible process" so microphone/camera permission prompts still belong to Discord.

var macVariants = []struct{ app, proc string }{
	{"Discord.app", "Discord"},
	{"Discord PTB.app", "Discord PTB"},
	{"Discord Canary.app", "Discord Canary"},
}

var macProcessNames = []string{"Discord", "Discord PTB", "Discord Canary"}

func macAppDirs() []string {
	if v := os.Getenv("DISCORDPROXY_MAC_APPS_DIR"); v != "" {
		return []string{v}
	}
	home, _ := os.UserHomeDir()
	return []string{"/Applications", filepath.Join(home, "Applications")}
}

// findDiscordMac returns the first Discord app bundle found in dirs (stable, then PTB, then Canary).
func findDiscordMac(dirs []string) (*install, error) {
	for _, v := range macVariants {
		for _, d := range dirs {
			p := filepath.Join(d, v.app)
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return &install{Name: strings.TrimSuffix(v.app, ".app"), Root: p, ExeName: v.proc}, nil
			}
		}
	}
	return nil, errNoDiscord
}

// launchCommandMac opens the app bundle with `open`, passing the proxy as a Chromium flag and as environment
// variables (the variables are for Discord's updater, which is not Chromium). `open --env` hands them to the app.
func launchCommandMac(in *install, port int) (name string, args []string) {
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	for _, kv := range proxyEnv(proxyURL) {
		args = append(args, "--env", kv)
	}
	args = append(args, "-a", in.Root, "--args", "--proxy-server="+proxyURL)
	return "/usr/bin/open", args
}

func discoverInstall() (*install, error) {
	if runtime.GOOS == "darwin" {
		return findDiscordMac(macAppDirs())
	}
	return findDiscord(localAppData())
}

// buildLaunch returns the command and extra environment that open Discord through the proxy on this OS.
func buildLaunch(in *install, port int) (name string, args []string, env []string) {
	if runtime.GOOS == "darwin" {
		name, args = launchCommandMac(in, port)
		return name, args, nil
	}
	return launchCommand(in, port)
}
