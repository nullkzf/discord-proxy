#!/usr/bin/env bash
# Build dist/DiscordProxy.exe (Windows, 64-bit) and dist/stubdiscord.exe (test stand-in).
#   scripts/build-windows.sh [version]            e.g. 1.0.0 (default 0.0.0-dev)
# Set DP_EMBED_RESOURCES=1 (CI does) to embed the icon, version info and manifest with goversioninfo.
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${1:-0.0.0-dev}"
CORE="${VERSION%%-*}"
IFS=. read -r MAJOR MINOR PATCH _ <<<"$CORE"
MAJOR="${MAJOR:-0}"; MINOR="${MINOR:-0}"; PATCH="${PATCH:-0}"
mkdir -p dist
rm -f cmd/discordproxy/rsrc_windows_amd64.syso

if [[ "${DP_EMBED_RESOURCES:-0}" == "1" ]]; then
  sed -e "s/__MAJOR__/$MAJOR/g" -e "s/__MINOR__/$MINOR/g" -e "s/__PATCH__/$PATCH/g" versioninfo.json.in > versioninfo.json
  go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1 -64 -o cmd/discordproxy/rsrc_windows_amd64.syso versioninfo.json
  rm -f versioninfo.json
fi

export GOOS=windows GOARCH=amd64 CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=${VERSION}" -o dist/DiscordProxy.exe ./cmd/discordproxy
go build -trimpath -ldflags "-s -w" -o dist/stubdiscord.exe ./tools/stubdiscord
rm -f cmd/discordproxy/rsrc_windows_amd64.syso
echo "built dist/DiscordProxy.exe ($VERSION)"
