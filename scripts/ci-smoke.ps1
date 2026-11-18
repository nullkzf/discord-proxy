# Runs on a real Windows machine (GitHub Actions). A stand-in for Discord (dist\stubdiscord.exe) is installed in a
# fake %LOCALAPPDATA%; the real DiscordProxy.exe is then run against it. Nothing here touches a real Discord.
$ErrorActionPreference = 'Stop'
$dist = Join-Path $PWD 'dist'
$tmp = Join-Path $env:RUNNER_TEMP 'dp-smoke'
Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
$la = Join-Path $tmp 'localappdata'; $state = Join-Path $tmp 'state'
New-Item -ItemType Directory "$la\Discord\app-1.0.9999" -Force | Out-Null
Copy-Item "$dist\stubdiscord.exe" "$la\Discord\Update.exe"
Copy-Item "$dist\stubdiscord.exe" "$la\Discord\app-1.0.9999\Discord.exe"

$env:DISCORDPROXY_LOCALAPPDATA = $la
$env:DISCORDPROXY_STATE_DIR = $state
$env:DISCORDPROXY_NO_UI = '1'
$env:DISCORDPROXY_IDLE_SECONDS = '4'
$env:DISCORDPROXY_DEBUG = '1'
$env:STUB_LIFETIME = '12'

function Fail($msg) {
    Write-Host "FAIL: $msg"
    if (Test-Path "$state\proxy.log") { Write-Host '--- proxy.log ---'; Get-Content "$state\proxy.log" }
    Get-Process DiscordProxy, Discord, stubdiscord -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    exit 1
}
function Launch {
    $p = Start-Process "$dist\DiscordProxy.exe" -PassThru
    $null = $p.Handle
    if (-not $p.WaitForExit(30000)) { Fail 'the launcher did not exit within 30 s' }
    return $p.ExitCode
}
function WaitFor($what, [scriptblock]$cond, $seconds = 20) {
    for ($i = 0; $i -lt $seconds * 5; $i++) { if (& $cond) { return } ; Start-Sleep -Milliseconds 200 }
    Fail "timed out waiting for: $what"
}
function Records($dir, $kind) { Get-ChildItem $dir -Filter "$kind-*.json" -ErrorAction SilentlyContinue }
function Json($f) { Get-Content $f.FullName -Raw | ConvertFrom-Json }
function EnvVal($rec, $name) { ($rec.env | Where-Object { $_.k -ceq $name } | Select-Object -First 1).v }

# ---- 1. launcher starts the proxy and "Discord" with the proxy flag + environment -----------------------
$env:STUB_RECORD = Join-Path $tmp 'rec1'
$code = Launch
if ($code -ne 0) { Fail "launcher exit code $code" }
WaitFor 'stub Discord.exe to start' { Records $env:STUB_RECORD 'discord' }
WaitFor 'proxy state file' { Test-Path "$state\proxy.json" }
$port = (Get-Content "$state\proxy.json" -Raw | ConvertFrom-Json).port
$flag = "--proxy-server=http://127.0.0.1:$port"
$u = Json (Records $env:STUB_RECORD 'update' | Select-Object -First 1)
$d = Json (Records $env:STUB_RECORD 'discord' | Select-Object -First 1)
if (($u.args -join ' ') -ne "--processStart Discord.exe --process-start-args $flag") { Fail "Update.exe was started with: $($u.args -join ' ')" }
if (($d.args -join ' ') -ne $flag) { Fail "Discord.exe was started with: $($d.args -join ' ')" }
foreach ($k in 'HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY') {
    if ((EnvVal $u $k) -ne "http://127.0.0.1:$port") { Fail "$k not passed to Update.exe: '$(EnvVal $u $k)'" }
    if ((EnvVal $d $k) -ne "http://127.0.0.1:$port") { Fail "$k not passed to Discord.exe: '$(EnvVal $d $k)'" }
}
Write-Host "OK 1: Discord was started with $flag and the proxy environment"

# ---- 2. the proxy really tunnels HTTPS and splits the handshake (on Windows) ------------------------------
$http = & curl.exe -s -o NUL -w '%{http_code}' -x "http://127.0.0.1:$port" --max-time 40 https://example.com/
if ($http -ne '200') { Fail "HTTPS through the proxy returned '$http'" }
$log = Get-Content "$state\proxy.log" -Raw
if ($log -notmatch 'split ClientHello for example\.com into 3 records') { Fail 'the TLS handshake to example.com was not split' }
Write-Host 'OK 2: HTTPS works through the proxy and the handshake is split'

# ---- 3. safety properties ------------------------------------------------------------------------------
$listen = Get-NetTCPConnection -State Listen -LocalPort $port
if (@($listen | Where-Object { $_.LocalAddress -ne '127.0.0.1' }).Count -gt 0) { Fail "proxy listens beyond 127.0.0.1: $($listen.LocalAddress -join ',')" }
$lb = & curl.exe -s -o NUL -w '%{http_code}' -x "http://127.0.0.1:$port" --max-time 15 http://127.0.0.1:9/
if ($lb -ne '403') { Fail "a request to loopback through the proxy returned '$lb' instead of 403" }
Write-Host 'OK 3: listens on 127.0.0.1 only and refuses loopback targets'

# ---- 4. a second launch reuses the same proxy ----------------------------------------------------------------
$env:STUB_RECORD = Join-Path $tmp 'rec2'
$null = Launch
WaitFor 'second Discord.exe record' { Records $env:STUB_RECORD 'discord' }
$port2 = (Get-Content "$state\proxy.json" -Raw | ConvertFrom-Json).port
if ($port2 -ne $port) { Fail "a second launch started another proxy ($port -> $port2)" }
Write-Host 'OK 4: a second launch reuses the running proxy'

# ---- 5. the proxy stops itself after Discord closes ---------------------------------------------------------
WaitFor 'the proxy to stop after Discord closed' { -not (Test-Path "$state\proxy.json") } 90
if (Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue) { Fail 'the port is still open after the proxy stopped' }
Write-Host 'OK 5: the proxy stopped by itself after Discord closed'

# ---- 6. Discord already open WITHOUT the proxy: it is restarted through the proxy ---------------------------
$env:STUB_LIFETIME = '300'
$old = Start-Process "$la\Discord\app-1.0.9999\Discord.exe" -PassThru
Start-Sleep -Seconds 1
$env:STUB_RECORD = Join-Path $tmp 'rec3'
$null = Launch
WaitFor 'the unproxied Discord to be closed' { -not (Get-Process -Id $old.Id -ErrorAction SilentlyContinue) }
WaitFor 'Discord to be reopened through the proxy' { Records $env:STUB_RECORD 'discord' }
$port3 = (Get-Content "$state\proxy.json" -Raw | ConvertFrom-Json).port
$d3 = Json (Records $env:STUB_RECORD 'discord' | Select-Object -First 1)
if (($d3.args -join ' ') -ne "--proxy-server=http://127.0.0.1:$port3") { Fail "reopened Discord args: $($d3.args -join ' ')" }
Write-Host 'OK 6: an already-open Discord is restarted through the proxy'

Get-Process DiscordProxy, Discord, stubdiscord -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Write-Host 'ALL SMOKE TESTS PASSED'
