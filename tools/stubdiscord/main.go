// Command stubdiscord stands in for Discord in tests. Built once and copied under two names:
//   - as Update.exe it records its arguments + proxy environment, then starts the sibling app-*\Discord.exe
//     with the value of --process-start-args (exactly what Squirrel's Update.exe does);
//   - as Discord.exe it records its arguments + proxy environment and stays alive for STUB_LIFETIME seconds.
//
// Records are JSON files in the directory named by STUB_RECORD.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func record(kind string) {
	dir := os.Getenv("STUB_RECORD")
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	// a list, not an object: PowerShell's JSON parser rejects keys that differ only by case (ALL_PROXY / all_proxy)
	var env []map[string]string
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy", "NO_PROXY", "no_proxy"} {
		env = append(env, map[string]string{"k": k, "v": os.Getenv(k)})
	}
	b, _ := json.MarshalIndent(map[string]any{"kind": kind, "args": os.Args[1:], "env": env, "pid": os.Getpid()}, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.json", kind, os.Getpid())), b, 0o644)
}

func main() {
	self := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	if self == "update" {
		record("update")
		var exeName, startArgs string
		for i, a := range os.Args {
			if a == "--processStart" && i+1 < len(os.Args) {
				exeName = os.Args[i+1]
			}
			if a == "--process-start-args" && i+1 < len(os.Args) {
				startArgs = os.Args[i+1]
			}
		}
		root := filepath.Dir(os.Args[0])
		apps, _ := filepath.Glob(filepath.Join(root, "app-*"))
		if exeName == "" || len(apps) == 0 {
			os.Exit(2)
		}
		cmd := exec.Command(filepath.Join(apps[len(apps)-1], exeName), startArgs)
		cmd.Env = os.Environ()
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		_ = cmd.Process.Release()
		return
	}
	record("discord")
	life := 15
	if n, err := strconv.Atoi(os.Getenv("STUB_LIFETIME")); err == nil {
		life = n
	}
	time.Sleep(time.Duration(life) * time.Second)
}
