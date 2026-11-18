package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// install describes one Discord installation (Squirrel layout: <root>\Update.exe and <root>\app-<version>\<Exe>).
type install struct {
	Name    string // "Discord", "DiscordPTB" or "DiscordCanary"
	Root    string // %LOCALAPPDATA%\Discord
	Update  string // Update.exe, or "" if missing
	Exe     string // newest app-*\Discord.exe
	ExeName string // Discord.exe, DiscordPTB.exe, DiscordCanary.exe
}

var variants = []struct{ dir, exe string }{
	{"Discord", "Discord.exe"},
	{"DiscordPTB", "DiscordPTB.exe"},
	{"DiscordCanary", "DiscordCanary.exe"},
}

// processNames are the executables whose presence means "Discord is running".
func processNames() []string {
	names := make([]string, 0, len(variants))
	for _, v := range variants {
		names = append(names, v.exe)
	}
	return names
}

var errNoDiscord = errors.New("Discord is not installed for this Windows user")

// findDiscord returns the first Discord variant installed under localAppData (stable, then PTB, then Canary).
func findDiscord(localAppData string) (*install, error) {
	for _, v := range variants {
		root := filepath.Join(localAppData, v.dir)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		bestVer, bestDir := []int(nil), ""
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "app-") {
				continue
			}
			ver, ok := parseVersion(strings.TrimPrefix(e.Name(), "app-"))
			if !ok {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, e.Name(), v.exe)); err != nil {
				continue
			}
			if bestDir == "" || cmpVersion(ver, bestVer) > 0 {
				bestVer, bestDir = ver, e.Name()
			}
		}
		if bestDir == "" {
			continue
		}
		in := &install{Name: v.dir, Root: root, Exe: filepath.Join(root, bestDir, v.exe), ExeName: v.exe}
		if up := filepath.Join(root, "Update.exe"); fileExists(up) {
			in.Update = up
		}
		return in, nil
	}
	return nil, errNoDiscord
}

func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }

func parseVersion(s string) ([]int, bool) {
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

func cmpVersion(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

// launchCommand builds the command that opens Discord through the proxy. Update.exe --processStart is Discord's
// own way to start the newest version; without Update.exe the newest app-*\Discord.exe is started directly.
// The proxy is passed as a Chromium flag (the app's web/voice-signalling traffic) and as environment variables
// (Discord's updater, which is not Chromium).
func launchCommand(in *install, port int) (name string, args []string, env []string) {
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	flag := "--proxy-server=" + proxyURL
	env = []string{
		"HTTP_PROXY=" + proxyURL, "HTTPS_PROXY=" + proxyURL, "ALL_PROXY=" + proxyURL,
		"http_proxy=" + proxyURL, "https_proxy=" + proxyURL, "all_proxy=" + proxyURL,
		"NO_PROXY=localhost,127.0.0.1", "no_proxy=localhost,127.0.0.1",
	}
	if in.Update != "" {
		return in.Update, []string{"--processStart", in.ExeName, "--process-start-args", flag}, env
	}
	return in.Exe, []string{flag}, env
}
