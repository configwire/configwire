#!/bin/sh
# ConfigWire entrypoint: fix legacy volume ownership, then drop to 10001.
#
# Images <= rc.12 ran as root, so existing /app/pb_data volumes may be
# root-owned. The app runs as UID/GID 10001; without a chown SQLite fails
# with "attempt to write a readonly database". Starts as root, chowns ONLY
# the data volume when its top dir is misowned, then execs via su-exec.
# Read-only rootfs stays untouched. Needs the CHOWN cap (see compose).
set -eu

DATA_DIR="/app/pb_data"

if [ "$(id -u)" = "0" ] && [ -d "$DATA_DIR" ]; then
    owner="$(stat -c '%u:%g' "$DATA_DIR" 2>/dev/null || echo unknown)"
    if [ "$owner" != "10001:10001" ]; then
        chown -R 10001:10001 "$DATA_DIR" 2>/dev/null || echo "configwire: entrypoint chown $DATA_DIR failed (needs CHOWN cap)" >&2
    fi
    exec su-exec 10001:10001 "$@"
else
    exec "$@"
fi
