#!/bin/sh
# Launched by NickelMenu (cmd_spawn) — NICKEL TAKEOVER MODE.
# Nickel and friends are killed for the session and restarted on any exit.
#
# Differences from listenlater's run.sh: dhcpcd/dhcpcd-dbus are SPARED —
# the diary needs Wi-Fi to reach its oracle. Have Wi-Fi connected before
# launching; with Nickel dead, nothing turns it off.
#
# NOTE: sickel is Kobo's watchdog daemon — killing nickel WITHOUT sickel
# gets the device rebooted. List of victims comes from KOReader.
BASE=/mnt/onboard/.adds/riddle
LOG="$BASE/log.txt"
echo "$(date): opening the diary (nickel takeover)" >> "$LOG"
sync

# NickelMenu quarantines its own library for ~20s after every nickel start
# and self-UNINSTALLS if nickel dies during that window. Killing nickel too
# soon after boot therefore silently removes NM (it got us twice). Wait for
# the failsafe to resolve before the kill.
t=0
while [ -e /usr/local/Kobo/imageformats/libnm.so.failsafe ]; do
    t=$((t + 1))
    [ "$t" -ge 30 ] && break
    sleep 1
done
[ "$t" -gt 0 ] && echo "$(date): waited ${t}s for NickelMenu failsafe" >> "$LOG"

killall -q -TERM nickel hindenburg sickel fickel strickel fontickel adobehost foxitpdf iink bluealsa bluetoothd fmon nanoclock.lua 2>/dev/null

# wait for nickel to actually die (max ~5s)
t=0
while pkill -0 nickel 2>/dev/null; do
    t=$((t + 1))
    [ "$t" -ge 20 ] && break
    usleep 250000
done
echo "$(date): nickel gone (waited $((t * 250))ms)" >> "$LOG"

# Nickel powers the Wi-Fi chip down as it dies — revive it, the diary needs
# its oracle. Nickel maintains the wpa_supplicant config with known networks
# (KOReader leans on the same file); the spared dhcpcd master then picks up
# the lease when the interface comes back.
if [ -e /dev/wmtWifi ] && [ "$(cat /sys/class/net/wlan0/operstate 2>/dev/null)" != "up" ]; then
    echo "$(date): reviving wifi" >> "$LOG"
    echo 1 > /dev/wmtWifi
    sleep 2
    ifconfig wlan0 up >> "$LOG" 2>&1
    if ! pkill -0 wpa_supplicant 2>/dev/null; then
        wpa_supplicant -D nl80211,wext -s -i wlan0 \
            -c /etc/wpa_supplicant/wpa_supplicant.conf \
            -C /var/run/wpa_supplicant -B >> "$LOG" 2>&1
    fi
    if ! pkill -0 dhcpcd 2>/dev/null; then
        dhcpcd wlan0 >> "$LOG" 2>&1 || udhcpc -S -i wlan0 -t15 -T10 -A3 -b -q >> "$LOG" 2>&1 &
    fi
    sleep 3
    echo "$(date): wifi state: $(cat /sys/class/net/wlan0/operstate 2>&1)" >> "$LOG"
fi

"$BASE/riddle" "$BASE" >> "$LOG" 2>&1
echo "$(date): the diary closed ($?) - restarting nickel" >> "$LOG"

exec /bin/sh "$BASE/restart-nickel.sh" >> "$LOG" 2>&1
