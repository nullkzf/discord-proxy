#!/usr/bin/env bash
# Mac smoke test: the real "Discord Proxy.app" is run against a stand-in for Discord (a throw-away Discord.app whose
# process is named StubDiscord, so a real Discord can never be matched or closed). Needs dist/Discord Proxy.app.
set -euo pipefail
cd "$(dirname "$0")/.."
BIN="$PWD/dist/Discord Proxy.app/Contents/MacOS/DiscordProxy"
[[ -x "$BIN" ]] || { echo "build first: scripts/build-mac.sh"; exit 1; }

TMP="$(mktemp -d)"
FAKE="$TMP/Applications"; STATE="$TMP/state"
export DISCORDPROXY_MAC_APPS_DIR="$FAKE" DISCORDPROXY_PROCESS_NAMES=StubDiscord DISCORDPROXY_STATE_DIR="$STATE" \
       DISCORDPROXY_NO_UI=1 DISCORDPROXY_IDLE_SECONDS=4 DISCORDPROXY_DEBUG=1 STUB_LIFETIME=12

cleanup() {
  pkill -x StubDiscord 2>/dev/null || true
  if [[ -f "$STATE/proxy.json" ]]; then kill "$(py 'import json,sys;print(json.load(open(sys.argv[1]))["pid"])' "$STATE/proxy.json")" 2>/dev/null || true; fi
  rm -rf "$TMP"
}
py() { python3 -c "$1" "${@:2}"; }
fail() { echo "FAIL: $*"; [[ -f "$STATE/proxy.log" ]] && { echo "--- proxy.log ---"; cat "$STATE/proxy.log"; }; cleanup; exit 1; }
wait_for() { local what=$1 secs=$2; shift 2; for ((i = 0; i < secs * 5; i++)); do "$@" && return 0; sleep 0.2; done; fail "timed out waiting for: $what"; }
have_record() { compgen -G "$1/discord-*.json" >/dev/null; }
port_of() { py 'import json,sys;print(json.load(open(sys.argv[1]))["port"])' "$STATE/proxy.json"; }
state_gone() { [[ ! -f "$STATE/proxy.json" ]]; }

# Every place a proxy or an auto-start entry could be installed behind the user's back, as one comparable text.
snapshot() {
  echo "system proxy:"; scutil --proxy
  echo "user LaunchAgents:"; ls -1 "$HOME/Library/LaunchAgents" 2>/dev/null || true
  echo "system LaunchAgents/Daemons mentioning discordproxy:"; ls -1 /Library/LaunchAgents /Library/LaunchDaemons 2>/dev/null | grep -i discordproxy || true
  echo "launchd jobs mentioning discordproxy:"; launchctl list 2>/dev/null | grep -i discordproxy | grep -vi teststub || true   # teststub = the stand-in Discord itself
  echo "crontab:"; crontab -l 2>&1 || true
}
SYSTEM_BEFORE="$(snapshot)"

# a throw-away Discord.app that runs the stand-in
go build -o "$TMP/StubDiscord" ./tools/stubdiscord
mkdir -p "$FAKE/Discord.app/Contents/MacOS"
cp "$TMP/StubDiscord" "$FAKE/Discord.app/Contents/MacOS/StubDiscord"
cat > "$FAKE/Discord.app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>StubDiscord</string>
<key>CFBundleIdentifier</key><string>dev.discordproxy.teststub</string>
<key>CFBundleName</key><string>StubDiscord</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSUIElement</key><true/>
</dict></plist>
PLIST
codesign --force -s - "$FAKE/Discord.app" >/dev/null 2>&1

# ---- 1. the launcher starts the proxy and opens "Discord" with the proxy flag + environment -------------
export STUB_RECORD="$TMP/rec1"
"$BIN" || fail "launcher exit code $?"
wait_for "stand-in Discord to start" 20 have_record "$STUB_RECORD"
wait_for "proxy state file" 10 test -f "$STATE/proxy.json"
PORT="$(port_of)"
REC="$(ls "$STUB_RECORD"/discord-*.json | head -1)"
ARGS="$(py 'import json,sys;print(" ".join(json.load(open(sys.argv[1]))["args"]))' "$REC")"
[[ "$ARGS" == "--proxy-server=http://127.0.0.1:$PORT" ]] || fail "Discord was started with: $ARGS"
for k in HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy all_proxy; do
  v="$(py 'import json,sys;d=json.load(open(sys.argv[1]));print([e["v"] for e in d["env"] if e["k"]==sys.argv[2]][0])' "$REC" "$k")"
  [[ "$v" == "http://127.0.0.1:$PORT" ]] || fail "$k not passed to Discord: '$v'"
done
echo "OK 1: Discord was opened with --proxy-server=http://127.0.0.1:$PORT and the proxy environment"

# ---- 2. the proxy really tunnels HTTPS and splits the handshake ------------------------------------------
CODE="$(curl -s -o /dev/null -w '%{http_code}' -x "http://127.0.0.1:$PORT" --max-time 40 https://example.com/ || true)"
[[ "$CODE" == "200" ]] || fail "HTTPS through the proxy returned '$CODE'"
grep -q 'split ClientHello for example.com into 3 records' "$STATE/proxy.log" || fail "the TLS handshake to example.com was not split"
echo "OK 2: HTTPS works through the proxy and the handshake is split"

# ---- 3. safety properties -----------------------------------------------------------------------------------
LISTEN="$(lsof -nP -iTCP:"$PORT" -sTCP:LISTEN || true)"
echo "$LISTEN" | grep -q "127.0.0.1:$PORT" || fail "not listening on 127.0.0.1: $LISTEN"
echo "$LISTEN" | grep -q "\*:$PORT" && fail "listening on all interfaces: $LISTEN"
LAN="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || true)"
if [[ -n "$LAN" ]] && nc -z -w 2 "$LAN" "$PORT" 2>/dev/null; then fail "the proxy answers on the LAN address $LAN:$PORT"; fi
LB="$(curl -s -o /dev/null -w '%{http_code}' -x "http://127.0.0.1:$PORT" --max-time 15 http://127.0.0.1:9/ || true)"
[[ "$LB" == "403" ]] || fail "a request to loopback through the proxy returned '$LB' instead of 403"
echo "OK 3: listens on 127.0.0.1 only (LAN address ${LAN:-n/a} refused) and refuses loopback targets"

# ---- 4. a second launch reuses the same proxy ------------------------------------------------------------------
# (macOS just brings an already-running app to the front, exactly like a second click on the real Discord, so there
# is no second stand-in process to look for: what matters is that the same proxy is reused.)
export STUB_RECORD="$TMP/rec2"
"$BIN" || fail "second launcher exit code $?"
sleep 1
[[ "$(port_of)" == "$PORT" ]] || fail "a second launch started another proxy"
[[ "$(grep -c 'opened Discord through the proxy on port '"$PORT" "$STATE/proxy.log")" -ge 2 ]] || fail "the second launch did not reuse port $PORT"
echo "OK 4: a second launch reuses the running proxy (port $PORT)"

# ---- 5. the proxy stops itself after Discord closes --------------------------------------------------------------
wait_for "the proxy to stop after Discord closed" 90 state_gone
nc -z -w 1 127.0.0.1 "$PORT" 2>/dev/null && fail "the port is still open after the proxy stopped"
echo "OK 5: the proxy stopped by itself after Discord closed"

# ---- 6. Discord already open WITHOUT the proxy: it is restarted through the proxy ------------------------------
STUB_LIFETIME=300 "$FAKE/Discord.app/Contents/MacOS/StubDiscord" &
OLD=$!
disown "$OLD" 2>/dev/null || true   # no 'Terminated' noise when the test closes it
sleep 1
export STUB_RECORD="$TMP/rec3"
STUB_LIFETIME=180 "$BIN" || fail "third launcher exit code $?"
gone() { ! kill -0 "$OLD" 2>/dev/null; }
wait_for "the unproxied Discord to be closed" 20 gone
wait_for "Discord to be reopened through the proxy" 20 have_record "$STUB_RECORD"
P3="$(port_of)"
ARGS3="$(py 'import json,sys;print(" ".join(json.load(open(sys.argv[1]))["args"]))' "$(ls "$STUB_RECORD"/discord-*.json | head -1)")"
[[ "$ARGS3" == "--proxy-server=http://127.0.0.1:$P3" ]] || fail "reopened Discord args: $ARGS3"
echo "OK 6: an already-open Discord is restarted through the proxy"

# ---- 7. only Discord is involved: nothing else on the Mac is pointed at the proxy or changed -----------------------
# (the proxy is running right now, from step 6)
[[ "$(snapshot)" == "$SYSTEM_BEFORE" ]] || { diff <(echo "$SYSTEM_BEFORE") <(snapshot) || true; fail "system settings changed while Discord Proxy ran"; }
grep -q 'example.com' "$STATE/proxy.log" || fail "control failed: the proxied request is missing from the log"
curl -s -o /dev/null --max-time 40 https://example.org/ || true   # a bystander program that is not Discord
grep -q 'example.org' "$STATE/proxy.log" && fail "a program that is not Discord went through the proxy"
echo "OK 7: system proxy, LaunchAgents and cron are unchanged; a bystander program does not use the proxy"

# ---- 8. the proxy writes only inside its own folder ------------------------------------------------------------------
HOMEDIR="$TMP/home"; mkdir -p "$HOMEDIR"
OWN="$HOMEDIR/Library/Application Support/DiscordProxy"
env -u DISCORDPROXY_STATE_DIR HOME="$HOMEDIR" DISCORDPROXY_STAY=1 "$BIN" --serve &
SERVE=$!
disown "$SERVE" 2>/dev/null || true
wait_for "a proxy started with an empty home folder" 10 test -f "$OWN/proxy.json"
P8="$(py 'import json,sys;print(json.load(open(sys.argv[1]))["port"])' "$OWN/proxy.json")"
[[ "$(curl -s -o /dev/null -w '%{http_code}' -x "http://127.0.0.1:$P8" --max-time 40 https://example.com/ || true)" == "200" ]] || fail "the second proxy did not tunnel HTTPS"
kill -TERM "$SERVE"
gone8() { ! kill -0 "$SERVE" 2>/dev/null; }
wait_for "the second proxy to stop" 10 gone8
OUTSIDE="$(find "$HOMEDIR" -type f ! -path "$OWN/*" || true)"
[[ -z "$OUTSIDE" ]] || fail "the proxy wrote files outside its own folder: $OUTSIDE"
[[ ! -f "$OWN/proxy.json" ]] || fail "the proxy left its state file behind after stopping"
echo "OK 8: the proxy wrote only inside Library/Application Support/DiscordProxy and cleaned up its state file"

cleanup
echo "ALL MAC SMOKE TESTS PASSED"
