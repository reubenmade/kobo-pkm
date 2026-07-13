#!/bin/sh
# Minimal Nickel restart, distilled from KOReader's platform/kobo/nickel.sh
# (AGPL) — the battle-tested resurrection incantation, minus KOReader- and
# Wi-Fi-specific handling (we never bring Wi-Fi up ourselves yet).
PATH="/sbin:/bin:/usr/sbin:/usr/bin:/usr/lib:"
export LD_LIBRARY_PATH="/usr/local/Kobo"
export QT_GSTREAMER_PLAYBIN_AUDIOSINK=alsasink
export QT_GSTREAMER_PLAYBIN_AUDIOSINK_DEVICE_PARAMETER=bluealsa:DEV=00:00:00:00:00:00
cd /
unset OLDPWD

# boot spinner; nickel kills it once it's up
/etc/init.d/on-animator.sh &

# recreate Nickel's hardware-status FIFO (udev writes into it)
rm -f /tmp/nickel-hardware-status
mkfifo /tmp/nickel-hardware-status

sync

/usr/local/Kobo/hindenburg &
LIBC_FATAL_STDERR_=1 /usr/local/Kobo/nickel -platform kobo -skipFontLoad &
udevadm trigger &

exit 0
