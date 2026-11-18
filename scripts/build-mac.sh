#!/usr/bin/env bash
# Build "dist/Discord Proxy.app" (universal: Apple Silicon + Intel, ad-hoc signed) and dist/DiscordProxy-<version>-mac.dmg.
#   scripts/build-mac.sh [version]        macOS only (uses lipo, sips, iconutil, codesign, hdiutil)
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${1:-0.0.0-dev}"
CORE="${VERSION%%-*}"
APP="dist/Discord Proxy.app"
rm -rf "$APP" dist/dmg dist/AppIcon.iconset
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

for arch in arm64 amd64; do
  GOOS=darwin GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o "dist/DiscordProxy-$arch" ./cmd/discordproxy
done
lipo -create -output "$APP/Contents/MacOS/DiscordProxy" dist/DiscordProxy-arm64 dist/DiscordProxy-amd64
rm -f dist/DiscordProxy-arm64 dist/DiscordProxy-amd64

ICONSET=dist/AppIcon.iconset
mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  sips -z "$s" "$s" assets/icon.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  sips -z "$((s * 2))" "$((s * 2))" assets/icon.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns -o "$APP/Contents/Resources/AppIcon.icns" "$ICONSET"
rm -rf "$ICONSET"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Discord Proxy</string>
  <key>CFBundleDisplayName</key><string>Discord Proxy</string>
  <key>CFBundleIdentifier</key><string>io.github.nullkzf.discordproxy</string>
  <key>CFBundleExecutable</key><string>DiscordProxy</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${CORE}</string>
  <key>CFBundleVersion</key><string>${CORE}</string>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
  <key>LSUIElement</key><true/>
  <key>NSHighResolutionCapable</key><true/>
  <key>LSApplicationCategoryType</key><string>public.app-category.utilities</string>
  <key>NSHumanReadableCopyright</key><string>MIT License. Unofficial; not affiliated with Discord Inc.</string>
</dict>
</plist>
PLIST

codesign --force --deep -s - "$APP"
codesign --verify --deep --strict "$APP"

mkdir -p dist/dmg
cp -R "$APP" dist/dmg/
ln -s /Applications dist/dmg/Applications
hdiutil create -volname "Discord Proxy" -srcfolder dist/dmg -ov -format UDZO "dist/DiscordProxy-${VERSION}-mac.dmg" >/dev/null
rm -rf dist/dmg
echo "built $APP and dist/DiscordProxy-${VERSION}-mac.dmg"
