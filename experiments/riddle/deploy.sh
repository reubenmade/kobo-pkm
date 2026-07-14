#!/bin/sh
# Build + copy the diary to a USB-connected Kobo.
set -eu

KOBO="${1:-/Volumes/KOBOeReader}"
HERE="$(cd "$(dirname "$0")" && pwd)"

if [ ! -d "$KOBO/.kobo" ]; then
    echo "error: no Kobo found at $KOBO (plugged in + tapped Connect?)" >&2
    exit 1
fi

echo "building…"
cd "$HERE"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags="-s -w" -o build/riddle .

DEST="$KOBO/.adds/riddle"
mkdir -p "$DEST"
cp build/riddle "$DEST/riddle"
cp run.sh "$DEST/run.sh"
cp restart-nickel.sh "$DEST/restart-nickel.sh"
chmod +x "$DEST/riddle" "$DEST/run.sh" "$DEST/restart-nickel.sh"
# don't clobber an existing (possibly calibrated / keyed) config
[ -f "$DEST/config.txt" ] || cp config.txt.example "$DEST/config.txt"

# NickelMenu entry alongside any existing ones
mkdir -p "$KOBO/.adds/nm"
cp "$HERE/nm-riddle" "$KOBO/.adds/nm/riddle"

echo "deployed. eject with: diskutil eject '$KOBO'"
echo "then: main menu (NickelMenu) -> 'The Diary'"
echo "remember: connect Wi-Fi BEFORE launching, and put your oracle_key in $DEST/config.txt"
