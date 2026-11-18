# Discord Proxy — for Windows and Mac

Opens **Discord** when your internet provider blocks it (for example by poisoning its DNS or cutting the connection
when it sees the site name). It is **not a VPN**: nothing is routed through another country, your IP address is the same,
and **only Discord** is pointed at it. No other app, browser or system setting is touched.

**Not sure what it does, or want to check it first?** Paste [`PASTE-TO-CLAUDE.md`](PASTE-TO-CLAUDE.md) into Claude (or any AI
assistant). It explains what this is, how it works, what it touches and what it can't do, in plain language, and tells the
assistant where to check each claim in the source.

> Unofficial. Not affiliated with, endorsed by or sponsored by Discord Inc. "Discord" is their trademark.
> The icon is an original drawing, not Discord's logo. Check what is allowed where you live and use it at your own risk.

## Install

### Windows 10 / 11
1. Open the **[latest release](../../releases/latest)** and download **`DiscordProxy-Setup-….exe`**.
2. Run it. Windows SmartScreen may say *"Windows protected your PC"* because the installer is not code-signed —
   click **More info → Run anyway**. It does not need administrator rights.
3. Open **Discord Proxy** (Start menu or the desktop shortcut) **instead of the normal Discord icon.**

### Mac (macOS 12 or newer, Apple Silicon or Intel)
1. Open the **[latest release](../../releases/latest)** and download **`DiscordProxy-….dmg`** (the one ending in `-mac.dmg`).
2. Open it and drag **Discord Proxy** into **Applications**.
3. The first time, macOS says it can't check the app, because it isn't notarized by Apple (that needs a paid developer
   account). To allow it: open **System Settings → Privacy & Security**, scroll to *Security*, and click **Open Anyway**
   next to "Discord Proxy". (Alternative, in Terminal: `xattr -dr com.apple.quarantine "/Applications/Discord Proxy.app"`.)
4. Open **Discord Proxy** instead of the normal Discord. It has no window or Dock icon; Discord just opens.

You need Discord itself installed first (from discord.com). Discord, Discord PTB and Discord Canary are supported.
If Discord is already open, Discord Proxy asks to restart it so it goes through the proxy. Your messages are on
Discord's servers, nothing is lost.

## Use

- Always start Discord from **Discord Proxy**. The normal Discord icon talks to the internet directly, so it will hang on
  "Checking for updates" or the loading screen while Discord is blocked.
- Windows: you can pin Discord Proxy to the taskbar. Mac: drag it to the Dock — it opens Discord and quits itself.
- Voice and video are unaffected; they don't go through the proxy at all.

## What it does

When you open Discord Proxy it
1. starts a tiny proxy in the background (no window) that listens on `127.0.0.1` only — it can't be reached from your
   network, only from programs on your own computer;
2. looks up Discord's servers over encrypted DNS (DNS-over-HTTPS to Cloudflare, with fallbacks) instead of your provider's
   DNS, which returns a block page;
3. splits the first message of each secure connection into several pieces, so a filter that reads the site name in the
   first message can't match it. It never decrypts anything and installs no certificate; and
4. opens Discord with `--proxy-server` and proxy environment variables, so only Discord uses it.

Nothing else is touched: no system proxy setting, no saved environment variable, no startup item, service or scheduled task,
no certificate. The tests check this on a real Windows machine and on a Mac by comparing those places before and after, and by
showing that another program's traffic does not go through the proxy (`scripts/ci-smoke.ps1`, `scripts/smoke-mac.sh`).

The proxy stops by itself a short while after Discord closes. A small log (errors and start/stop events; a failed connection is logged with its host name, so look it over before posting it publicly) is in
`%LOCALAPPDATA%\DiscordProxy\proxy.log` (Windows) or `~/Library/Application Support/DiscordProxy/proxy.log` (Mac).

## Limits and honest warnings

- It can't beat a block on a server's IP address itself (that needs a VPN or a server abroad), or filtering that works
  in other ways.
- While it runs, any program on **your own** computer could use the proxy (it is local-only, but it has no password).
- Discord's own updater gets the proxy through environment variables. If Discord ever sticks on "Checking for updates" even
  when opened from Discord Proxy, please open an issue with the end of `proxy.log`.
- Antivirus programs sometimes distrust small unsigned programs. The installers are built automatically on GitHub from this
  source (see *Actions*), and each release lists SHA-256 checksums.
- What was tested:
  - the proxy against a real blocking network: with the handshake splitting Discord's site, gateway, CDN and update servers
    open; with it switched off the same connections are cut;
  - the launcher, installer, uninstaller and icon on a real Windows machine (GitHub Actions) and the Mac app on a real Mac,
    all against a stand-in for Discord;
  - unit tests (including a server that rejects the split handshake, to check the automatic plain retry).
  **Not tested: the real Discord for Windows, or the real Discord for Mac on a second machine, under a blocking network.**
  If it doesn't work for you, that is the most valuable thing to report.

## Uninstall

- Windows: Settings → Apps → *Discord Proxy* → Uninstall (removes the program, its shortcuts and its small data folder).
- Mac: drag **Discord Proxy** from Applications to the Trash, and delete `~/Library/Application Support/DiscordProxy`.

## Build it yourself

```
go test ./...                          # unit tests, any OS
bash scripts/build-windows.sh 1.0.0    # dist/DiscordProxy.exe (cross-compiles from macOS/Linux too)
bash scripts/build-mac.sh 1.0.0        # dist/Discord Proxy.app (universal) + .dmg — macOS only
bash scripts/smoke-mac.sh              # Mac smoke test against a stand-in for Discord
```
`DP_EMBED_RESOURCES=1` additionally embeds the Windows icon, version info and manifest (uses
[goversioninfo](https://github.com/josephspurrier/goversioninfo)); the Windows installer is `installer/DiscordProxy.iss`
(Inno Setup 6). CI (`.github/workflows/ci.yml`) builds and tests Windows on every push and publishes tagged releases; the Mac job is run by hand (GitHub's hosted Mac machines were unavailable), so the Mac app is built and tested locally.

Layout: `internal/proxy` (the proxy: DoH lookup, handshake splitting, tunnelling), `cmd/discordproxy` (launcher + background
service for both OSes), `tools/stubdiscord` (stand-in for Discord in tests), `installer/`, `scripts/`, `assets/`.

## License

MIT — see [LICENSE](LICENSE).
