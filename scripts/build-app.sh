#!/usr/bin/env bash
# Builds dist/SockDock.app (menu bar GUI + bundled sockdock CLI).
# Requires: Go, Node (npm), the wails3 CLI (for bindings), Xcode command line tools.
set -euo pipefail

cd "$(dirname "$0")/.."
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="${OUT:-dist}"
APP="$OUT/SockDock.app"
BUNDLE_ID="io.github.iwamatsu0430.sockdock"
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-13.0}"
export CGO_CFLAGS="-mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"
export CGO_LDFLAGS="-mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"

echo "==> bindings"
if command -v wails3 >/dev/null; then
  wails3 generate bindings -silent -ts -d gui/frontend/bindings ./gui
else
  echo "    wails3 not found; using the committed bindings"
fi

echo "==> frontend"
(cd gui/frontend && npm ci --no-audit --no-fund --silent && npm run build --silent)
touch gui/frontend/dist/.gitkeep

echo "==> go build ($VERSION)"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
LDFLAGS="-s -w -X main.version=$VERSION"
go build -trimpath -ldflags "$LDFLAGS" -o "$APP/Contents/MacOS/sockdock" ./cmd/sockdock
go build -trimpath -tags production -ldflags "$LDFLAGS" -o "$APP/Contents/MacOS/sockdock-gui" ./gui

echo "==> icon"
ICONSET="$(mktemp -d)/icon.iconset"
go run ./internal/brand/mkicon "$ICONSET"
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icon.icns"

echo "==> Info.plist"
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>SockDock</string>
  <key>CFBundleDisplayName</key><string>SockDock</string>
  <key>CFBundleIdentifier</key><string>$BUNDLE_ID</string>
  <key>CFBundleExecutable</key><string>sockdock-gui</string>
  <key>CFBundleIconFile</key><string>icon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${VERSION#v}</string>
  <key>CFBundleVersion</key><string>${VERSION#v}</string>
  <key>LSMinimumSystemVersion</key><string>$MACOSX_DEPLOYMENT_TARGET</string>
  <key>LSUIElement</key><true/>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSHumanReadableCopyright</key><string>MIT License. Not affiliated with Slack Technologies, LLC.</string>
</dict>
</plist>
PLIST

echo "==> ad-hoc codesign"
codesign --force --deep --sign - "$APP" >/dev/null

echo "built $APP"
