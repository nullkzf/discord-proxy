package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nullkzf/discord-proxy-windows/internal/proxy"
)

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindDiscordPicksTheNewestVersionNumerically(t *testing.T) {
	la := t.TempDir()
	touch(t, filepath.Join(la, "Discord", "Update.exe"))
	touch(t, filepath.Join(la, "Discord", "app-1.0.9", "Discord.exe"))
	touch(t, filepath.Join(la, "Discord", "app-1.0.10", "Discord.exe")) // 10 > 9 (a string sort would get this wrong)
	touch(t, filepath.Join(la, "Discord", "app-1.0.11", "other.txt"))   // no exe: ignored
	touch(t, filepath.Join(la, "Discord", "app-notaversion", "Discord.exe"))
	in, err := findDiscord(la)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(in.Exe)) != "app-1.0.10" || in.ExeName != "Discord.exe" {
		t.Errorf("picked %s", in.Exe)
	}
	if in.Update != filepath.Join(la, "Discord", "Update.exe") {
		t.Errorf("Update.exe not found: %q", in.Update)
	}
}

func TestFindDiscordFallsBackToPTBAndFailsCleanly(t *testing.T) {
	la := t.TempDir()
	if _, err := findDiscord(la); err != errNoDiscord {
		t.Fatalf("want errNoDiscord, got %v", err)
	}
	touch(t, filepath.Join(la, "DiscordPTB", "app-1.0.1", "DiscordPTB.exe"))
	in, err := findDiscord(la)
	if err != nil || in.Name != "DiscordPTB" || in.Update != "" {
		t.Fatalf("got %+v %v", in, err)
	}
	touch(t, filepath.Join(la, "Discord", "app-1.0.1", "Discord.exe")) // stable wins over PTB
	if in, _ := findDiscord(la); in.Name != "Discord" {
		t.Errorf("stable should win, got %s", in.Name)
	}
}

func TestLaunchCommandUsesUpdateExeAndSetsProxyEverywhere(t *testing.T) {
	in := &install{Update: `C:\x\Update.exe`, Exe: `C:\x\app-1\Discord.exe`, ExeName: "Discord.exe"}
	name, args, env := launchCommand(in, 4321)
	if name != in.Update {
		t.Errorf("name %q", name)
	}
	want := []string{"--processStart", "Discord.exe", "--process-start-args", "--proxy-server=http://127.0.0.1:4321"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Errorf("args %v", args)
	}
	joined := strings.Join(env, "\n")
	for _, k := range []string{"HTTP_PROXY=http://127.0.0.1:4321", "HTTPS_PROXY=http://127.0.0.1:4321", "https_proxy=http://127.0.0.1:4321", "NO_PROXY=localhost,127.0.0.1"} {
		if !strings.Contains(joined, k) {
			t.Errorf("env is missing %s", k)
		}
	}
	// without Update.exe the newest Discord.exe is started directly
	in.Update = ""
	name, args, _ = launchCommand(in, 4321)
	if name != in.Exe || len(args) != 1 || args[0] != "--proxy-server=http://127.0.0.1:4321" {
		t.Errorf("direct launch wrong: %q %v", name, args)
	}
}

func TestProbeRecognisesOnlyOurProxy(t *testing.T) {
	srv := proxy.New(proxy.Config{})
	addr, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_, ps, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	if !probe(port) {
		t.Error("our own proxy should be recognised")
	}
	// some other program on a port: answers 200 to everything
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
			c.Close()
		}
	}()
	_, ps2, _ := net.SplitHostPort(ln.Addr().String())
	other, _ := strconv.Atoi(ps2)
	if probe(other) {
		t.Error("a stranger on another port must not be mistaken for the proxy")
	}
	if probe(1) {
		t.Error("a closed port is not the proxy")
	}
}

func TestStateRoundTripAndEnsureProxyReuse(t *testing.T) {
	t.Setenv("DISCORDPROXY_STATE_DIR", t.TempDir())
	if _, ok := readState(); ok {
		t.Fatal("no state expected yet")
	}
	srv := proxy.New(proxy.Config{})
	addr, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_, ps, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	if err := writeState(port); err != nil {
		t.Fatal(err)
	}
	st, ok := readState()
	if !ok || st.Port != port || st.PID != os.Getpid() {
		t.Fatalf("state %+v ok=%v", st, ok)
	}
	got, reused, err := ensureProxy()
	if err != nil || !reused || got != port {
		t.Fatalf("ensureProxy = %d reused=%v err=%v", got, reused, err)
	}
}
