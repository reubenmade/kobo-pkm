#!/bin/sh
# Launched by NickelMenu (cmd_spawn).
# NOTE: Do NOT SIGSTOP nickel — it feeds the hardware watchdog, and freezing
# it reboots the device. The app instead grabs the touchscreen, buttons, and
# accelerometer so Nickel receives no input while we run.
BASE=/mnt/onboard/.adds/listenlater
LOG="$BASE/log.txt"
echo "$(date): launching listenlater" >> "$LOG"
# diagnostic: does nickel hold /dev/watchdog?
NPID=$(pidof nickel 2>/dev/null | cut -d' ' -f1)
[ -n "$NPID" ] && ls -l "/proc/$NPID/fd" 2>/dev/null | grep -i watchdog >> "$LOG"
# diagnostic: full input device table (name + key capability bitmaps),
# to locate the physical page-turn buttons
if [ ! -f "$BASE/input-devices.txt" ]; then
    cat /proc/bus/input/devices > "$BASE/input-devices.txt" 2>&1
fi
"$BASE/listenlater" "$BASE" >> "$LOG" 2>&1
echo "$(date): listenlater exited ($?)" >> "$LOG"
