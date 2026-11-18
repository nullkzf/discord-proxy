# Shared by the Windows CI scripts. Get-SystemSnapshot records every place a proxy (or an auto-start entry) could be
# installed behind the user's back; the tests compare it before and after to prove nothing outside Discord changed.
function Get-SystemSnapshot {
    $lines = @()
    $is = Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' -ErrorAction SilentlyContinue
    $lines += "WinINET ProxyEnable=$($is.ProxyEnable) ProxyServer=$($is.ProxyServer) AutoConfigURL=$($is.AutoConfigURL) AutoDetect=$($is.AutoDetect)"
    $lines += 'WinHTTP: ' + ((& netsh winhttp show proxy) -join ' | ')
    $envKey = Get-ItemProperty 'HKCU:\Environment' -ErrorAction SilentlyContinue
    $proxyVars = @()
    if ($envKey) { $proxyVars = @($envKey.PSObject.Properties | Where-Object { $_.Name -match 'proxy' } | ForEach-Object { "$($_.Name)=$($_.Value)" }) }
    $lines += 'user environment proxy variables: ' + ($proxyVars -join ';')
    $run = Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -ErrorAction SilentlyContinue
    $runNames = @()
    if ($run) { $runNames = @($run.PSObject.Properties | Where-Object { $_.Name -notmatch '^PS' } | ForEach-Object { $_.Name }) }
    $lines += 'auto-start entries: ' + ($runNames -join ';')
    $tasks = @(Get-ScheduledTask -ErrorAction SilentlyContinue | Where-Object { $_.TaskName -match 'discord' } | ForEach-Object { $_.TaskName })
    $lines += 'scheduled tasks mentioning discord: ' + ($tasks -join ';')
    return ($lines -join "`n")
}
