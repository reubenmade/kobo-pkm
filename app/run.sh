#!/bin/sh
# Launched by NickelMenu (cmd_spawn) — NICKEL TAKEOVER MODE.
# Nickel and friends are killed for the session (frees the physical page
# buttons, stops sleep-timer interference) and restarted on any exit.
# NOTE: sickel is Kobo's watchdog daemon — killing nickel WITHOUT sickel
# gets the device rebooted. List of victims comes from KOReader.
BASE=/mnt/onboard/.adds/listenlater
LOG="$BASE/log.txt"
echo "$(date): launching listenlater (nickel takeover)" >> "$LOG"
sync

killall -q -TERM nickel hindenburg sickel fickel strickel fontickel adobehost foxitpdf iink dhcpcd-dbus dhcpcd bluealsa bluetoothd fmon nanoclock.lua 2>/dev/null

# wait for nickel to actually die (max ~5s)
t=0
while pkill -0 nickel 2>/dev/null; do
    t=$((t + 1))
    [ "$t" -ge 20 ] && break
    usleep 250000
done
echo "$(date): nickel gone (waited $((t * 250))ms)" >> "$LOG"

"$BASE/listenlater" "$BASE" >> "$LOG" 2>&1
echo "$(date): listenlater exited ($?) - restarting nickel" >> "$LOG"

exec /bin/sh "$BASE/restart-nickel.sh" >> "$LOG" 2>&1
