#!/usr/bin/env bash
# Writes Formula/sockdock.rb and Casks/sockdock.rb for a published release into
# a checkout of the Homebrew tap (github.com/iwamatsu0430/homebrew-tap).
#
#   scripts/homebrew-tap.sh 0.1.0 ../homebrew-tap
#
# The release v0.1.0 must already be on GitHub: the sha256 values are taken
# from the source tarball and SockDock-0.1.0.zip it serves. Pass APP_ZIP=path
# to hash a local zip instead of downloading it.
set -euo pipefail

VERSION="${1:?usage: $0 VERSION TAP_DIR}"
VERSION="${VERSION#v}"
TAP="${2:?usage: $0 VERSION TAP_DIR}"
REPO="iwamatsu0430/sock-dock-slack"
HERE="$(cd "$(dirname "$0")/.." && pwd)"

sha_url() { curl -fsSL "$1" | shasum -a 256 | cut -d' ' -f1; }

SOURCE_SHA256="$(sha_url "https://github.com/$REPO/archive/refs/tags/v$VERSION.tar.gz")"
if [ -n "${APP_ZIP:-}" ]; then
  APP_SHA256="$(shasum -a 256 "$APP_ZIP" | cut -d' ' -f1)"
else
  APP_SHA256="$(sha_url "https://github.com/$REPO/releases/download/v$VERSION/SockDock-$VERSION.zip")"
fi

render() { # template out
  mkdir -p "$(dirname "$2")"
  grep -v '^# \(Template for\|scripts/homebrew-tap.sh fills\)' "$1" |
    sed -e "s/@VERSION@/$VERSION/g" \
        -e "s/@SOURCE_SHA256@/$SOURCE_SHA256/g" \
        -e "s/@APP_SHA256@/$APP_SHA256/g" >"$2"
}

render "$HERE/packaging/homebrew/sockdock.rb" "$TAP/Formula/sockdock.rb"
render "$HERE/packaging/homebrew/sockdock-cask.rb" "$TAP/Casks/sockdock.rb"
echo "wrote $TAP/Formula/sockdock.rb and $TAP/Casks/sockdock.rb (v$VERSION)"
