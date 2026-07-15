#!/bin/sh
# Build + copy Tangle to a USB-connected Kobo. The Nickel-takeover scripts are
# generated from the shared kit templates (kit/scripts/*.tmpl), so every
# experiment ships the same battle-tested run/restart logic.
set -eu

APP=tangle
KOBO="${1:-/Volumes/KOBOeReader}"
HERE="$(cd "$(dirname "$0")" && pwd)"
KIT="$HERE/../../kit"

if [ ! -d "$KOBO/.kobo" ]; then
    echo "error: no Kobo found at $KOBO (plugged in + tapped Connect?)" >&2
    exit 1
fi

echo "building $APP (arm)…"
cd "$HERE"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags="-s -w" -o build/$APP .

# Generate the takeover scripts from the kit templates.
mkdir -p build
sed "s/@APP@/$APP/g" "$KIT/scripts/run.sh.tmpl" > build/run.sh
sed "s/@APP@/$APP/g" "$KIT/scripts/restart-nickel.sh.tmpl" > build/restart-nickel.sh

DEST="$KOBO/.adds/$APP"
mkdir -p "$DEST"
cp build/$APP "$DEST/$APP"
cp build/run.sh "$DEST/run.sh"
cp build/restart-nickel.sh "$DEST/restart-nickel.sh"
chmod +x "$DEST/$APP" "$DEST/run.sh" "$DEST/restart-nickel.sh"
# Don't clobber an existing (possibly calibrated) config.
[ -f "$DEST/config.txt" ] || cp config.txt.example "$DEST/config.txt"

# NickelMenu entry alongside any existing ones.
mkdir -p "$KOBO/.adds/nm"
cp "$HERE/nm-$APP" "$KOBO/.adds/nm/$APP"

echo "deployed to $DEST"
echo "eject with: diskutil eject '$KOBO'"
echo "wait ~30s (Nickel is mid content-import), then: NickelMenu -> 'Tangle'"
