#!/bin/sh
# Build + copy the native app to a USB-connected Kobo.
set -eu

KOBO="${1:-/Volumes/KOBOeReader}"
HERE="$(cd "$(dirname "$0")" && pwd)"

if [ ! -d "$KOBO/.kobo" ]; then
    echo "error: no Kobo found at $KOBO (plugged in + tapped Connect?)" >&2
    exit 1
fi

echo "building…"
cd "$HERE/app"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags="-s -w" -o build/listenlater .

DEST="$KOBO/.adds/listenlater"
mkdir -p "$DEST"
cp build/listenlater "$DEST/listenlater"
cp run.sh "$DEST/run.sh"
cp restart-nickel.sh "$DEST/restart-nickel.sh"
cp "$HERE/vendor/fbink" "$DEST/fbink" 2>/dev/null || true
cp "$HERE/vendor/fbdepth" "$DEST/fbdepth" 2>/dev/null || true
chmod +x "$DEST/listenlater" "$DEST/run.sh" "$DEST/fbink" "$DEST/fbdepth" 2>/dev/null || true
# don't clobber an existing (possibly calibrated) config
[ -f "$DEST/config.txt" ] || cp config.txt "$DEST/config.txt"

mkdir -p "$KOBO/.adds/nm"
cp "$HERE/nm-config/dashboard" "$KOBO/.adds/nm/dashboard"

echo "deployed. eject with: diskutil eject '$KOBO'"
echo "then: main menu (NickelMenu) -> 'Listen Later'"
