#!/bin/sh
# ConfigWire entrypoint: fix legacy volume ownership, then drop to 10001.
#
# Images <= rc.12 ran as root, so existing /app/pb_data volumes may be
# root-owned. The app runs as UID/GID 10001; without a chown SQLite fails
# with "attempt to write a readonly database". Starts as root, chowns the
# data volume when anything inside it is misowned or unwritable, verifies
# writability as 10001, then execs via su-exec. The chown touches only the
# data volume, never the (possibly read-only) rootfs. Needs the CHOWN cap
# (see compose). Bind mounts must be pre-owned by 10001:10001 on the host.
set -eu

TARGET_UID="10001"
TARGET_GID="10001"
DEFAULT_DATA_DIR="/app/pb_data"
DATA_DIR="$DEFAULT_DATA_DIR"

# Honor the same --dir flag the app uses so custom data dirs get the
# same fixup instead of silently falling through to a SQLite 8 failure.
prev=""
for arg in "$@"; do
    if [ "$prev" = "--dir" ]; then
        DATA_DIR="$arg"
        break
    fi
    case "$arg" in
        --dir=*) DATA_DIR="${arg#--dir=}"; break ;;
        --dir) prev="--dir" ;;
    esac
done

fail_unwritable() {
    echo "configwire: data dir $DATA_DIR is not writable by $TARGET_UID:$TARGET_GID." >&2
    echo "configwire: SQLite cannot start (attempt to write a readonly database)." >&2
    echo "configwire: fix: chown -R $TARGET_UID:$TARGET_GID $DATA_DIR (on the host for bind mounts), then restart." >&2
    echo "configwire: upgrading from rc.12 or older with a named volume needs cap_add [CHOWN] for the one-time auto-chown (see docker-compose.yml)." >&2
    exit 1
}

# try_chown runs the recursive fixup and surfaces the kernel's reason on
# failure (e.g. "Operation not permitted" = missing CHOWN cap,
# "Read-only file system" = volume mounted ro). Swallowing stderr here
# is what used to turn every case into the same cryptic SQLite 8 loop.
try_chown() {
    if chown_err="$(chown -R "$TARGET_UID:$TARGET_GID" "$DATA_DIR" 2>&1)"; then
        return 0
    fi
    echo "configwire: entrypoint chown $DATA_DIR failed: $chown_err" >&2
    echo "configwire: (missing cap_add [CHOWN], a read-only mount, or a bind mount needing a host-side chown)" >&2
    return 1
}

# db_files_owned reports whether every existing SQLite file is already
# owned by the target UID:GID. A 10001-owned dir with a root-owned
# data.db passes a touch probe (new files are fine) yet SQLite still
# fails opening the db itself, so ownership of the db files is checked
# explicitly instead of relying on the probe alone.
db_files_owned() {
    for f in "$DATA_DIR"/data.db "$DATA_DIR"/data.db-wal "$DATA_DIR"/data.db-shm "$DATA_DIR"/data.db-journal; do
        if [ -e "$f" ]; then
            owner="$(stat -c '%u:%g' "$f" 2>/dev/null || echo unknown)"
            if [ "$owner" != "$TARGET_UID:$TARGET_GID" ]; then
                return 1
            fi
        fi
    done
    return 0
}

# Writability probe as the target UID (works when started as root via
# su-exec; when already UID 10001 the plain touch is the same check).
probe_writable() {
    if [ "$(id -u)" = "0" ]; then
        su-exec "$TARGET_UID:$TARGET_GID" touch "$DATA_DIR/.writetest" 2>/dev/null
    else
        touch "$DATA_DIR/.writetest" 2>/dev/null
    fi
}

if [ "$(id -u)" = "0" ]; then
    if [ ! -e "$DATA_DIR" ]; then
        mkdir -p "$DATA_DIR"
        chown "$TARGET_UID:$TARGET_GID" "$DATA_DIR" 2>/dev/null || fail_unwritable
    fi
    if [ -d "$DATA_DIR" ]; then
        owner="$(stat -c '%u:%g' "$DATA_DIR" 2>/dev/null || echo unknown)"
        # Repair when the top dir is misowned, when any db file is
        # misowned (partial chown: dir looks fixed, data.db is still
        # root-owned), or when the 10001 probe cannot create files.
        if [ "$owner" != "$TARGET_UID:$TARGET_GID" ] || ! db_files_owned || ! probe_writable; then
            if ! try_chown; then
                fail_unwritable
            fi
        fi
        rm -f "$DATA_DIR/.writetest" 2>/dev/null || true
        if ! probe_writable; then
            rm -f "$DATA_DIR/.writetest" 2>/dev/null || true
            fail_unwritable
        fi
        rm -f "$DATA_DIR/.writetest" 2>/dev/null || true
        exec su-exec "$TARGET_UID:$TARGET_GID" "$@"
    else
        echo "configwire: $DATA_DIR exists but is not a directory" >&2
        exit 1
    fi
else
    # Already dropped (docker --user, k8s runAsUser): no chown possible,
    # so fail fast with the fix instead of a cryptic SQLite 8 loop.
    if [ ! -d "$DATA_DIR" ]; then
        echo "configwire: data dir $DATA_DIR missing and running as uid $(id -u) (cannot chown); create it owned by $TARGET_UID:$TARGET_GID" >&2
        exit 1
    fi
    if ! probe_writable; then
        rm -f "$DATA_DIR/.writetest" 2>/dev/null || true
        fail_unwritable
    fi
    rm -f "$DATA_DIR/.writetest" 2>/dev/null || true
    exec "$@"
fi
