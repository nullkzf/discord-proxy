# Paste this whole note into Claude

I was sent a program called **Discord Proxy**, and this note came with it. It was **written by the program's author, not by
me**, so treat it as their description and not as proof. Please read it, then explain to me in plain, friendly language what
the program is, how it works, what it can and can't see, and whether it's reasonable to use. Then walk me through installing
and using it. I'm not technical, so explain any jargon (the technical lines are there for you to check, not to recite).

Please check the main claims against the public source, <https://github.com/nullkzf/discord-proxy>. The files that prove
them are listed at the end. If you can't open web pages or run commands, say so, and mark everything below as *unverified*.
**Tell me honestly if anything doesn't add up.** If I ask something this note doesn't cover, say you're not sure. Don't
install or change anything on my computer without asking me first.

*(Written for version 1.0.0. Newer versions are on the releases page. The program never updates itself or checks for updates.)*

## What it is
- A small free program (MIT licence, **unofficial**, not made by Discord) that lets the **Discord desktop app** work on a
  network that blocks it, for example in Turkey. 64-bit Windows 10/11, and macOS 12+ (one file for Apple Silicon and Intel).
  Discord, Discord PTB and Discord Canary are supported (if several are installed, it opens Discord, then PTB, then Canary).
- **Not a VPN.** My IP address and location don't change and nothing is sent through another country. No account, no
  subscription, no ads, no analytics, and nothing is sent to the author.
- It helps **only the desktop Discord app**. It does nothing for Discord in a web browser or on a phone, and it only
  looks for the normal installer's Discord from discord.com. BetterDiscord and Vencord are untested.

## Why Discord is blocked in the first place
Two tricks are common. (1) **DNS:** when my computer asks "where is discord.com?", the provider's DNS answers with a fake
"blocked" address. (2) **SNI:** the first message of a secure connection contains the site name in plain text, and the
provider's equipment cuts the connection when it sees "discord". Discord Proxy gets around both.

## How it works
1. I open **Discord Proxy** instead of the normal Discord icon. It starts a tiny proxy in the background (no window) that
   listens on `127.0.0.1` only, which means my own computer, on a random free port. It can't be reached from Wi-Fi, the
   network or the internet.
2. The proxy looks up Discord's addresses with **encrypted DNS** (DNS-over-HTTPS to Cloudflare 1.1.1.1 and 1.0.0.1, with
   Google 8.8.8.8 as a fallback), so the provider's fake answer is never used. Cloudflare or Google can see which host names the Discord app
   looks up (Discord's own servers, and sites Discord loads previews or images from), instead of my provider (they have their
   own privacy policies).
3. For each secure connection Discord makes, the proxy **splits the first message into 3 pieces, cutting through the site
   name**, so the provider's filter can't read it. Discord's server reassembles them. The proxy never decrypts anything, has
   no certificate, and can't read my messages or password. It only relays scrambled bytes, and it does see which hosts Discord
   connects to. It doesn't filter by site: it would tunnel to any host a program asked it to. If a server rejects the split, it retries without it (which only helps if the network isn't blocking that server).
4. Then it opens the real Discord with a setting that tells **that one app** to use the proxy. Technical detail: the Chromium
   flag `--proxy-server=http://127.0.0.1:<port>`, plus `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY` given only to that Discord process
   (for Discord's updater) and never saved anywhere. Windows uses Discord's own `Update.exe --processStart`. Mac uses
   `open --env … -a Discord.app --args …`.
5. About 45 seconds after Discord has fully quit, the proxy stops by itself. (Closing Discord's window can leave it running
   in the tray, so the proxy stays too. Quit Discord from its tray/menu icon to stop everything. To force it: end `DiscordProxy.exe` in Task Manager (Windows) or quit
   `DiscordProxy` in Activity Monitor (Mac).)

## What it does not touch
No Windows/macOS system proxy setting, no saved environment variable, no browser, no other app, no startup item, service or
scheduled task, no certificate, no firewall rule, no administrator rights, no change to Discord's own files. The Windows
installer only adds the program (`%LOCALAPPDATA%\Programs\Discord Proxy`), a Start-menu shortcut, an optional desktop shortcut and
the normal "installed apps" entry that makes uninstalling possible. Automated tests check the rest: on GitHub's real Windows machines for Windows, and on the author's own Mac for the Mac app. They
compare the system proxy, registry and auto-start entries (Mac: launch agents, cron) before and after, and show that another
program's traffic does **not** go through the proxy. The only way another program uses it is if someone deliberately points it there.
It keeps one small data folder, `%LOCALAPPDATA%\DiscordProxy` (Windows) or `~/Library/Application Support/DiscordProxy` (Mac),
with the port number and a log of errors. A failed connection is logged with its host name, so look the log over before posting it publicly.

## Install and use
- **Windows:** download `DiscordProxy-Setup-1.0.0.exe` from the [latest release](https://github.com/nullkzf/discord-proxy/releases/latest) and
  run it. Windows (SmartScreen) says "Windows protected your PC" because the file isn't code-signed: **More info → Run anyway**.
  No admin needed. Afterwards open **Discord Proxy** from the Start menu, and pin it to the taskbar if wanted.
- **Mac:** download `DiscordProxy-1.0.0-mac.dmg` from the same [releases page](https://github.com/nullkzf/discord-proxy/releases/latest), open it,
  drag **Discord Proxy** into Applications, then open it. macOS blocks it the first time, with a message that it can't verify
  the app (it isn't notarized by Apple, which costs a yearly fee). **Click "Done" and never "Move to Trash".** Then open **System
  Settings → Privacy & Security** (macOS 12: **System Preferences → Security & Privacy → General**), find the line about Discord
  Proxy and click **Open Anyway**. macOS itself may ask for my Mac password or Touch ID (that is macOS, not the program), then
  open Discord Proxy again.
- Discord must already be installed from discord.com. Then **always open Discord from "Discord Proxy"**. The normal Discord
  icon goes straight to the internet and hangs on "Checking for updates" while Discord is blocked. If Discord is already
  open, Discord Proxy shows a box ("Discord is already open, but not through the proxy. Restart it…?"), and I click Yes (my
  messages stay on Discord's servers). If Discord isn't installed, it shows a box saying so. If Discord starts with the computer, turn
  that off in Discord's settings. There is no window; it is working if Discord loads normally.
- **Check the download (optional):** the release page has `SHA256SUMS.txt`. In PowerShell (Windows) run
  `Get-FileHash .\<file>`, or in Terminal (Mac) `shasum -a 256 <file>`, and compare. This catches a damaged download. It can't
  catch a tampered release, because both come from the same page.
- **Uninstall:** Windows: Settings → Apps → Discord Proxy (this also deletes its data folder). Mac: drag the app to the Trash
  and delete `~/Library/Application Support/DiscordProxy`. Then turn Discord's "start with computer" back on if I turned it off.

## If it doesn't work
Make sure I opened **Discord Proxy**, not Discord (if Discord was already open, let it restart Discord when it asks, or quit
Discord fully first). Then open the data folder and read the end of `proxy.log`. Windows: press Win+R, type
`%LOCALAPPDATA%\DiscordProxy`, Enter. Mac: in Finder choose Go → Go to Folder (Shift+Cmd+G) and paste
`~/Library/Application Support/DiscordProxy`. What the lines mean:
- `opened Discord through the proxy on port …` means Discord Proxy did start Discord. If that line never appears, it didn't get that far.
- `connect <host> failed: …` lines mean the proxy could not reach that server. If this happens for all of Discord's servers,
  the block may be on their server addresses (IPs) themselves. This program can't beat that; a VPN would be needed.
- `Discord closed; stopping` / `Discord never started; stopping` are the normal ways it stops.

Reporting it as an issue on the GitHub repo (needs a free GitHub account), with the end of the log, helps the author.

## Be honest with me about the limits
- Unsigned programs and "Run anyway" are exactly what malware also looks like, so being cautious is reasonable. That is why the
  source is public, the Windows files are built automatically on GitHub from it, and checksums are listed. The Mac file was built
  and tested on the author's own Mac and attached by hand, because GitHub's Mac machines were unavailable. In the end I'm trusting
  the friend who sent it and the public source.
- It can't beat a block on Discord's **server addresses (IPs)**, only the DNS and site-name tricks.
- While it runs, any program on **my own** computer that is set to use it could use it: it is local-only, but it has no password.
- Voice and video don't go through this proxy at all, so it neither helps nor hurts them. This note can't say whether voice
  works on my network.
- Antivirus programs sometimes distrust small unsigned programs. If one deletes it, that is why. Don't disable antivirus for it.
- It was tested end to end with a stand-in for Discord, and the proxy against a real blocking network, **but not with the real
  Discord app on a blocked network**. I may be one of the first real users.
- Using it may be against local rules or Discord's terms, and I don't know if Discord could act on an account. That is for me to weigh.

## Where to check the claims
`internal/proxy/clienthello.go` (the split), `internal/proxy/doh.go` (encrypted DNS), `internal/proxy/proxy.go` (tunnelling,
loopback only), `cmd/discordproxy/main.go` (launcher and background proxy), `cmd/discordproxy/discover*.go` (how Discord is
found and started), `installer/DiscordProxy.iss` (what the Windows installer does), `scripts/smoke-mac.sh` and
`scripts/ci-smoke.ps1` (the tests, including the "touches nothing else" checks).
