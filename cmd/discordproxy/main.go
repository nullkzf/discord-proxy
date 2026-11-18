// Command discordproxy opens Discord through a small local proxy that gets past DNS poisoning and SNI-based
// blocking. Only Discord is pointed at the proxy; nothing else on the PC is touched, no system setting is changed.
//
//	DiscordProxy.exe            open Discord through the proxy (starts the background proxy if needed)
//	DiscordProxy.exe --serve    run the proxy itself (started automatically, hidden; exits after Discord closes)
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nullkzf/discord-proxy-windows/internal/proxy"
)

var version = "dev"

const appTitle = "Discord Proxy"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--serve":
			os.Exit(runServe())
		case "--version":
			fmt.Println(version)
			return
		}
	}
	os.Exit(runLauncher())
}

// ---- environment (every knob exists so tests can run without touching a real Discord) --------------

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

func localAppData() string {
	if v := os.Getenv("DISCORDPROXY_LOCALAPPDATA"); v != "" {
		return v
	}
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		return v
	}
	d, _ := os.UserCacheDir()
	return d
}

func stateDir() string {
	if v := os.Getenv("DISCORDPROXY_STATE_DIR"); v != "" {
		return v
	}
	return filepath.Join(localAppData(), "DiscordProxy")
}

func noUI() bool { return os.Getenv("DISCORDPROXY_NO_UI") == "1" }

// ---- state file + log ------------------------------------------------------------------------------

type state struct {
	Port    int   `json:"port"`
	PID     int   `json:"pid"`
	Started int64 `json:"started"`
}

func statePath() string { return filepath.Join(stateDir(), "proxy.json") }
func logPath() string   { return filepath.Join(stateDir(), "proxy.log") }

func readState() (state, bool) {
	b, err := os.ReadFile(statePath())
	var st state
	if err != nil || json.Unmarshal(b, &st) != nil || st.Port <= 0 {
		return state{}, false
	}
	return st, true
}

func writeState(port int) error {
	b, _ := json.Marshal(state{Port: port, PID: os.Getpid(), Started: time.Now().Unix()})
	tmp := statePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, statePath())
}

// openLog keeps a small local log (errors and lifecycle). Host names are only logged with DISCORDPROXY_DEBUG=1.
func openLog() (func(string, ...any), io.Closer) {
	_ = os.MkdirAll(stateDir(), 0o700)
	if st, err := os.Stat(logPath()); err == nil && st.Size() > 512*1024 {
		_ = os.Remove(logPath())
	}
	f, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return func(string, ...any) {}, io.NopCloser(nil)
	}
	debug := os.Getenv("DISCORDPROXY_DEBUG") == "1"
	return func(format string, a ...any) {
		if !debug && (strings.HasPrefix(format, "split ClientHello") || strings.HasPrefix(format, "retrying")) {
			return
		}
		fmt.Fprintf(f, time.Now().Format("2006-01-02 15:04:05 ")+format+"\n", a...)
	}, f
}

// probe reports whether a Discord Proxy server is listening on port: it asks it to CONNECT to loopback,
// which this proxy always refuses with 403 (so a stranger on a recycled port is not mistaken for it).
func probe(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 800*time.Millisecond)
	if err != nil {
		return false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	if _, err := io.WriteString(c, "CONNECT localhost:9 HTTP/1.1\r\nHost: localhost:9\r\n\r\n"); err != nil {
		return false
	}
	buf := make([]byte, 12)
	n, _ := io.ReadFull(c, buf)
	return n == 12 && string(buf) == "HTTP/1.1 403"
}

// ---- the background proxy --------------------------------------------------------------------------

func runServe() int {
	logf, closer := openLog()
	defer closer.Close()
	if st, ok := readState(); ok && probe(st.Port) {
		return 0 // one is already serving
	}
	srv := proxy.New(proxy.Config{
		Listen:   os.Getenv("DISCORDPROXY_LISTEN"),
		Fragment: os.Getenv("DISCORDPROXY_FRAGMENT") != "0",
		Logf:     logf,
	})
	addr, err := srv.Start()
	if err != nil {
		logf("could not start: %v", err)
		return 1
	}
	_, ps, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	if err := writeState(port); err != nil {
		logf("could not write state: %v", err)
		srv.Close()
		return 1
	}
	logf("Discord Proxy %s listening on %s", version, addr)
	defer func() {
		if st, ok := readState(); ok && st.PID == os.Getpid() {
			_ = os.Remove(statePath())
		}
		srv.Close()
		logf("stopped")
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	// Stay up while Discord runs. Exit when it hasn't been seen for a while (or never showed up).
	idle := time.Duration(envInt("DISCORDPROXY_IDLE_SECONDS", 45)) * time.Second
	grace := time.Duration(envInt("DISCORDPROXY_GRACE_SECONDS", 120)) * time.Second
	stay := os.Getenv("DISCORDPROXY_STAY") == "1"
	start, lastSeen, seen := time.Now(), time.Time{}, false
	tick := time.NewTicker(1500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-sig:
			return 0
		case now := <-tick.C:
			running := processRunning(processNames()...)
			if running {
				seen, lastSeen = true, now
			}
			if stay {
				continue
			}
			if !seen && now.Sub(start) > grace {
				logf("Discord never started; stopping")
				return 0
			}
			if seen && !running && now.Sub(lastSeen) > idle {
				logf("Discord closed; stopping")
				return 0
			}
		}
	}
}

// ensureProxy makes sure a proxy is serving and returns its port; reused is true if one was already running.
func ensureProxy() (port int, reused bool, err error) {
	if st, ok := readState(); ok && probe(st.Port) {
		return st.Port, true, nil
	}
	self, err := os.Executable()
	if err != nil {
		return 0, false, err
	}
	if err := spawnDetached(self, "--serve"); err != nil {
		return 0, false, fmt.Errorf("could not start the background proxy: %w", err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := readState(); ok && probe(st.Port) {
			return st.Port, false, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, false, errors.New("the proxy did not start in time. Details: " + logPath())
}

// ---- the launcher ----------------------------------------------------------------------------------

func runLauncher() int {
	logf, closer := openLog()
	defer closer.Close()

	in, err := findDiscord(localAppData())
	if err != nil {
		notify("Discord isn't installed for this Windows user.\n\nInstall Discord from discord.com first, then open Discord Proxy again.")
		return 1
	}
	port, reused, err := ensureProxy()
	if err != nil {
		logf("launcher: %v", err)
		notify("Discord Proxy could not start its proxy.\n\n" + err.Error())
		return 1
	}
	if processRunning(processNames()...) && !reused {
		if !confirm("Discord is already open, but not through the proxy.\n\nRestart it so it uses the proxy? Your messages are kept on Discord's servers, nothing is lost.") {
			return 0
		}
		killProcesses(processNames()...)
		for i := 0; i < 40 && processRunning(processNames()...); i++ {
			time.Sleep(200 * time.Millisecond)
		}
	}
	name, args, env := launchCommand(in, port)
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		logf("launcher: start %s: %v", name, err)
		notify("Discord Proxy could not open Discord.\n\n" + err.Error())
		return 1
	}
	logf("opened %s through the proxy on port %d", in.Name, port)
	return 0
}
