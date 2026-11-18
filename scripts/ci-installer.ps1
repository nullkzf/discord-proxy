# Runs on a real Windows machine (GitHub Actions): silent install, check the result, silent uninstall.
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
. (Join-Path $PSScriptRoot 'ci-common.ps1')
$setup = Get-ChildItem dist -Filter 'DiscordProxy-Setup-*.exe' | Select-Object -First 1
if (-not $setup) { throw 'installer not found in dist\' }
$dir = Join-Path $env:RUNNER_TEMP 'dp-install'
Remove-Item $dir -Recurse -Force -ErrorAction SilentlyContinue

$systemBefore = Get-SystemSnapshot
$p = Start-Process $setup.FullName -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/DIR=$dir", '/TASKS=desktopicon' -PassThru
$null = $p.Handle
$p.WaitForExit()
if ($p.ExitCode -ne 0) { throw "installer exit code $($p.ExitCode)" }

$exe = Join-Path $dir 'DiscordProxy.exe'
if (-not (Test-Path $exe)) { throw 'DiscordProxy.exe was not installed' }
foreach ($f in 'README.md', 'LICENSE', 'unins000.exe') { if (-not (Test-Path (Join-Path $dir $f))) { throw "$f was not installed" } }

$sh = New-Object -ComObject WScript.Shell
$startMenu = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Discord Proxy.lnk'
$desktop = Join-Path ([Environment]::GetFolderPath('Desktop')) 'Discord Proxy.lnk'
foreach ($lnk in $startMenu, $desktop) {
    if (-not (Test-Path $lnk)) { throw "shortcut missing: $lnk" }
    if ($sh.CreateShortcut($lnk).TargetPath -ne $exe) { throw "shortcut $lnk points to $($sh.CreateShortcut($lnk).TargetPath)" }
}

$vi = (Get-Item $exe).VersionInfo
if ($vi.ProductName -ne 'Discord Proxy') { throw "ProductName is '$($vi.ProductName)'" }
if ($vi.FileDescription -ne 'Discord Proxy') { throw "FileDescription is '$($vi.FileDescription)'" }

# the embedded icon must be the green one
$bmp = [System.Drawing.Icon]::ExtractAssociatedIcon($exe).ToBitmap()
$r = 0; $g = 0; $b = 0; $n = 0
for ($x = 0; $x -lt $bmp.Width; $x++) { for ($y = 0; $y -lt $bmp.Height; $y++) {
    $c = $bmp.GetPixel($x, $y); if ($c.A -gt 200) { $r += $c.R; $g += $c.G; $b += $c.B; $n++ } } }
if ($n -eq 0) { throw 'the .exe has no icon' }
$r = [int]($r / $n); $g = [int]($g / $n); $b = [int]($b / $n)
Write-Host "average icon colour: R=$r G=$g B=$b"
if (-not ($g -gt $r + 15 -and $g -gt $b + 15)) { throw 'the embedded icon is not green' }

$ver = Start-Process $exe -ArgumentList '--version' -PassThru -Wait -RedirectStandardOutput (Join-Path $env:RUNNER_TEMP 'ver.txt')
Write-Host "version output: $((Get-Content (Join-Path $env:RUNNER_TEMP 'ver.txt') -ErrorAction SilentlyContinue) -join ' ')"
if ((Get-SystemSnapshot) -ne $systemBefore) { throw 'installing changed a system proxy setting, the environment or an auto-start entry' }
Write-Host 'OK: installed, shortcuts, version info and the green icon are all in place'

$u = Start-Process (Join-Path $dir 'unins000.exe') -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -PassThru
$null = $u.Handle
$u.WaitForExit()
Start-Sleep -Seconds 2
if (Test-Path $exe) { throw 'uninstall left DiscordProxy.exe behind' }
foreach ($lnk in $startMenu, $desktop) { if (Test-Path $lnk) { throw "uninstall left $lnk behind" } }
if ((Get-SystemSnapshot) -ne $systemBefore) { throw 'uninstalling left a system setting changed' }
Write-Host 'OK: uninstalled cleanly'
