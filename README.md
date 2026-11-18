# Discord Proxy for Windows

Opens **Discord** on Windows when your internet provider blocks it (for example by poisoning its DNS or cutting the
connection when it sees the site name). It is **not a VPN**: nothing is routed through another country, your IP address
is the same, and **only Discord** is pointed at it. No other app, browser or system setting is touched.

> Unofficial. Not affiliated with, endorsed by or sponsored by Discord Inc. "Discord" is their trademark.
> The icon is an original drawing, not Discord's logo. Check what is allowed where you live and use it at your own risk.

## Install

1. Go to the **[Releases](../../releases/latest)** page and download **`DiscordProxy-Setup-….exe`**.
2. Run it. Windows SmartScreen may say *"Windows protected your PC"* because the installer is not code-signed —
   click **More info → Run anyway**. It does not need administrator rights.
3. Open **Discord Proxy** (Start menu or the desktop shortcut) **instead of the normal Discord icon.**

If Discord is already open, Discord Proxy asks to restart it so it goes through the proxy. Your messages are on
Discord's servers, nothing is lost.

You need Discord itself installed first (from discord.com). Discord, Discord PTB and Discord Canary are supported.

## Use

- Always start Discord from **Discord Proxy**. The normal Discord icon talks to the internet directly, so it will
  hang on "Checking for updates" or the loading screen while Discord is blocked.
- You can pin Discord Proxy to the taskbar like any program.
- Voice and video are unaffected. They don't go through the proxy at all.

## What it does

When you open Discord Proxy it
1. starts a tiny proxy in the background (no window) that listens on `127.0.0.1` only — it can't be reached from your
   network, only from programs on your own PC;
2. looks up Discord's servers over encrypted DNS (DNS-over-HTTPS to Cloudflare, with fallbacks) instead of your provider's
   DNS, which returns a block page; and
3. splits the first message of each secure connection into several pieces, so a filter that reads the site name in the
   first message can't match it. It never decrypts anything and installs no certificate;
4. opens Discord with `--proxy-server` and proxy environment variables, so only Discord uses it.

The proxy stops by itself a short while after Discord closes. Logs are in `%LOCALAPPDATA%\DiscordProxy\proxy.log`
(errors only; no sites you visit).

## Limits and honest warnings

- It can't beat a block on a server's IP address itself (that needs a VPN or a server abroad), or a block that
  also filters the connection in other ways.
- While it runs, any program on **your own** PC could use the proxy (it is local-only, but it has no password).
- Discord's own updater: Discord Proxy passes the proxy to it through environment variables. If Discord ever gets stuck on
  "Checking for updates" even when opened from Discord Proxy, please open an issue with the end of `proxy.log`.
- Antivirus programs sometimes distrust small unsigned programs. The installer and exe are built automatically on
  GitHub from this source (see *Actions*), and each release lists SHA-256 checksums.
- Tested: the proxy against a real blocking network (it opens Discord's sites and APIs where a plain connection is cut off);
  the launcher, installer, uninstaller and icon on real Windows machines in GitHub Actions with a stand-in for Discord.
  **Not tested here: the real Discord for Windows under a blocking network.** If it doesn't work for you, that is the
  most valuable thing to report.

## Uninstall

Settings → Apps → *Discord Proxy* → Uninstall. It removes the program, its shortcuts and its small data folder.

## Build it yourself

```
go test ./...                      # unit tests, any OS
bash scripts/build-windows.sh 1.0.0 # dist/DiscordProxy.exe (cross-compiles from macOS/Linux too)
```
`DP_EMBED_RESOURCES=1` additionally embeds the icon, version info and manifest (uses
[goversioninfo](https://github.com/josephspurrier/goversioninfo)); the installer script is `installer/DiscordProxy.iss`
(Inno Setup 6). CI (`.github/workflows/ci.yml`) does all of this on Windows and publishes tagged releases.

Layout: `internal/proxy` (the proxy: DoH lookup, handshake splitting, tunnelling), `cmd/discordproxy` (launcher + background
service), `tools/stubdiscord` (stand-in for Discord in tests), `installer/`, `scripts/`.

## License

MIT — see [LICENSE](LICENSE).
