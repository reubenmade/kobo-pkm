#!/bin/sh
# Install NickelMenu + the dashboard POC onto a USB-connected Kobo.
# Safe to re-run: copying KoboRoot.tgz again just reinstalls NickelMenu,
# and the dashboard/config copies are idempotent.
set -eu

KOBO="${1:-/Volumes/KOBOeReader}"
HERE="$(cd "$(dirname "$0")" && pwd)"

if [ ! -d "$KOBO/.kobo" ]; then
    echo "error: no Kobo found at $KOBO (is it plugged in and did you tap Connect?)" >&2
    exit 1
fi

echo "Kobo firmware: $(cat "$KOBO/.kobo/version" 2>/dev/null || echo unknown)"

# 1. NickelMenu — Kobo installs anything named .kobo/KoboRoot.tgz on eject,
#    then reboots. Skip if NickelMenu is already installed (it deletes the
#    tgz after install and leaves .adds/nm/ behind).
if [ -d "$KOBO/.adds/nm" ]; then
    echo "NickelMenu already installed — skipping KoboRoot.tgz"
else
    cp "$HERE/vendor/KoboRoot-nickelmenu-v0.6.0.tgz" "$KOBO/.kobo/KoboRoot.tgz"
    echo "Staged NickelMenu installer at .kobo/KoboRoot.tgz"
fi

# 2. Dashboard page
mkdir -p "$KOBO/.adds/dashboard"
cp "$HERE/dashboard/index.html" "$KOBO/.adds/dashboard/index.html"
echo "Copied dashboard to .adds/dashboard/"

# 3. NickelMenu config (harmless to copy before NM is installed;
#    it reads configs from .adds/nm/ once running)
mkdir -p "$KOBO/.adds/nm"
cp "$HERE/nm-config/dashboard" "$KOBO/.adds/nm/dashboard"
echo "Copied NickelMenu config to .adds/nm/dashboard"

echo
echo "Done. Eject the Kobo (don't just unplug):"
echo "  diskutil eject '$KOBO'"
echo "The Kobo will install NickelMenu and reboot. Afterwards, open the"
echo "main menu (top-left ☰ on newer firmware) -> 'Dashboard'."
