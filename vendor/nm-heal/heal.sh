#!/bin/sh
# Restore a NickelMenu library stranded by its own crash-failsafe rename
# (NM renames libnm.so -> libnm.so.failsafe on every nickel start and back
# ~20s later; power loss inside the window strands it and NM never loads).
# Runs at boot via udev (loop0 = onboard partition), before nickel starts.
NM=/usr/local/Kobo/imageformats/libnm.so
if [ -e "$NM.failsafe" ] && [ ! -e "$NM" ]; then
    mv "$NM.failsafe" "$NM"
    logger -t nm-heal "restored stranded NickelMenu failsafe" 2>/dev/null || true
fi
exit 0
