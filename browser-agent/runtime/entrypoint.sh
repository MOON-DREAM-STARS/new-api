#!/bin/sh
set -eu

log() {
    printf '%s\n' "[web-workspace-runtime] $*"
}

WW_WORKSPACE_DIR="${WW_WORKSPACE_DIR:-/workspace}"
WW_DISPLAY="${WW_DISPLAY:-:99}"
WW_SCREEN_WIDTH="${WW_SCREEN_WIDTH:-1280}"
WW_SCREEN_HEIGHT="${WW_SCREEN_HEIGHT:-720}"
WW_VNC_PORT="${WW_VNC_PORT:-5900}"
WW_PROVIDER="${WW_PROVIDER:-unknown}"
WW_START_URL="${WW_START_URL:-}"

# The runtime window is the provider application window: it opens on the
# provider shell with no tab strip, no address bar and no warning bar. A
# provider without a start page keeps the empty window instead of guessing.
if [ -z "$WW_START_URL" ]; then
    case "$WW_PROVIDER" in
        chatgpt) WW_START_URL="https://chatgpt.com/" ;;
        *) WW_START_URL="about:blank" ;;
    esac
fi

export WW_START_URL
if [ -z "${WW_PROXY_SERVER:-}" ]; then
    log "WW_PROXY_SERVER is required and must be the egress proxy as http(s)://host[:port]"
    exit 1
fi

WW_GUARD_MODE="$(printf '%s' "${WW_GUARD_MODE:-LOCKED}" | tr -d '[:space:]' | tr '[:lower:]' '[:upper:]')"
case "$WW_GUARD_MODE" in
    LOCKED|LOGIN) ;;
    *)
        log "invalid WW_GUARD_MODE: expected LOCKED or LOGIN"
        exit 1
        ;;
esac
export WW_GUARD_MODE

export HOME="$WW_WORKSPACE_DIR"
export DISPLAY="$WW_DISPLAY"
export XDG_CONFIG_HOME="$WW_WORKSPACE_DIR/profile/config"
export XDG_CACHE_HOME="$WW_WORKSPACE_DIR/cache"
export XDG_DATA_HOME="$WW_WORKSPACE_DIR/profile/data"
export XDG_RUNTIME_DIR="$WW_WORKSPACE_DIR/tmp/runtime"

XVFB_PID=""
X11VNC_PID=""
CHROMIUM_PID=""
GUARD_PID=""

terminate_process() {
    pid="$1"
    name="$2"

    [ -n "$pid" ] || return 0

    kill -TERM "$pid" 2>/dev/null || true
    pkill -TERM -x "$name" 2>/dev/null || true

    i=0
    while pgrep -x "$name" >/dev/null 2>&1 && [ "$i" -lt 50 ]; do
        sleep 0.1 2>/dev/null || sleep 1
        i=$((i + 1))
    done

    if pgrep -x "$name" >/dev/null 2>&1; then
        pkill -KILL -x "$name" 2>/dev/null || true
    fi

    wait "$pid" 2>/dev/null || true
}

# process_running reports whether a tracked PID is still alive. A shell child
# that already exited stays visible as a zombie until it is reaped, so the
# watchdog cannot rely on kill -0 alone.
process_running() {
    [ -n "$1" ] || return 1
    state="$(awk '{ print $3 }' "/proc/$1/stat" 2>/dev/null)" || true
    [ -n "$state" ] && [ "$state" != "Z" ]
}

cleanup() {
    status=$?
    trap - EXIT INT TERM

    log "shutting down: workspace-guard -> chromium -> x11vnc -> Xvfb"
    terminate_process "$GUARD_PID" workspace-guard
    terminate_process "$CHROMIUM_PID" chromium
    terminate_process "$X11VNC_PID" x11vnc
    terminate_process "$XVFB_PID" Xvfb
    log "shutdown complete"

    exit "$status"
}

trap cleanup EXIT INT TERM

log "starting provider=${WW_PROVIDER} workspace=${WW_WORKSPACE_DIR} display=${WW_DISPLAY} screen=${WW_SCREEN_WIDTH}x${WW_SCREEN_HEIGHT} vnc_port=${WW_VNC_PORT} guard_mode=${WW_GUARD_MODE} start_url=${WW_START_URL}"

umask 077
mkdir -p "$WW_WORKSPACE_DIR/profile" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$XDG_DATA_HOME" "$XDG_RUNTIME_DIR"
chmod 700 "$XDG_RUNTIME_DIR" 2>/dev/null || true

mkdir -p /tmp/.X11-unix
chmod 1777 /tmp/.X11-unix 2>/dev/null || true

if pgrep -x chromium >/dev/null 2>&1; then
    log "refusing to start while a chromium process is already running"
    exit 1
fi

for lock in SingletonLock SingletonCookie SingletonSocket; do
    lock_path="$WW_WORKSPACE_DIR/profile/$lock"
    if [ -e "$lock_path" ] || [ -L "$lock_path" ]; then
        log "removing stale Chromium profile lock $lock"
        rm -f "$lock_path"
    fi
done

log "starting Xvfb"
Xvfb "$WW_DISPLAY" -screen 0 "${WW_SCREEN_WIDTH}x${WW_SCREEN_HEIGHT}x24" -nolisten tcp -ac &
XVFB_PID=$!

display_number="${WW_DISPLAY#:}"
display_number="${display_number%%.*}"
display_socket="/tmp/.X11-unix/X${display_number}"

i=0
while [ ! -S "$display_socket" ]; do
    if ! kill -0 "$XVFB_PID" 2>/dev/null; then
        log "Xvfb exited before $display_socket became ready"
        exit 1
    fi
    i=$((i + 1))
    if [ "$i" -ge 100 ]; then
        log "timed out waiting for $display_socket"
        exit 1
    fi
    sleep 0.1 2>/dev/null || sleep 1
done
log "Xvfb ready at $display_socket"

log "starting x11vnc"
x11vnc -display "$WW_DISPLAY" -rfbport "$WW_VNC_PORT" -forever -shared -nopw -nolookup -noxdamage -quiet -bg -o /tmp/x11vnc.log

i=0
while [ -z "$X11VNC_PID" ]; do
    X11VNC_PID="$(pidof x11vnc 2>/dev/null || true)"
    if [ -n "$X11VNC_PID" ]; then
        X11VNC_PID="${X11VNC_PID%% *}"
        break
    fi
    i=$((i + 1))
    if [ "$i" -ge 100 ]; then
        log "timed out waiting for x11vnc to start"
        [ -f /tmp/x11vnc.log ] && cat /tmp/x11vnc.log >&2
        exit 1
    fi
    sleep 0.1 2>/dev/null || sleep 1
done
i=0
while ! netstat -ltn 2>/dev/null | grep -q ":${WW_VNC_PORT}[[:space:]]"; do
    if ! pgrep -x x11vnc >/dev/null 2>&1; then
        log "x11vnc exited before port $WW_VNC_PORT became ready"
        [ -f /tmp/x11vnc.log ] && cat /tmp/x11vnc.log >&2
        exit 1
    fi
    i=$((i + 1))
    if [ "$i" -ge 100 ]; then
        log "timed out waiting for x11vnc port $WW_VNC_PORT"
        exit 1
    fi
    sleep 0.1 2>/dev/null || sleep 1
done
log "x11vnc ready pid=$X11VNC_PID port=$WW_VNC_PORT"

log "starting chromium with profile=$WW_WORKSPACE_DIR/profile guard_mode=$WW_GUARD_MODE"
chromium --no-sandbox \
    --test-type \
    --user-data-dir="$WW_WORKSPACE_DIR/profile" \
    --display="$WW_DISPLAY" \
    --no-first-run \
    --no-default-browser-check \
    --disable-features=TranslateUI \
    --window-size="${WW_SCREEN_WIDTH},${WW_SCREEN_HEIGHT}" \
    --window-position=0,0 \
    --proxy-server="$WW_PROXY_SERVER" \
    --proxy-bypass-list="<-loopback>" \
    --disable-quic \
    --webrtc-ip-handling-policy=disable_non_proxied_udp \
    --remote-debugging-port=9222 \
    --remote-debugging-address=127.0.0.1 \
    --deny-permission-prompts \
    --app="$WW_START_URL" &
CHROMIUM_PID=$!

log "starting workspace-guard mode=$WW_GUARD_MODE"
workspace-guard &
GUARD_PID=$!

# The guard is the only CDP consumer of this runtime: without it Chromium has
# no navigation, network, popup, download or clipboard enforcement, so a guard
# that stops must terminate the whole runtime.
while :; do
    if ! process_running "$GUARD_PID"; then
        GUARD_STATUS=0
        wait "$GUARD_PID" 2>/dev/null || GUARD_STATUS=$?
        log "workspace-guard exited with status ${GUARD_STATUS}: failing closed"
        exit 1
    fi
    if ! process_running "$CHROMIUM_PID"; then
        CHROMIUM_STATUS=0
        wait "$CHROMIUM_PID" 2>/dev/null || CHROMIUM_STATUS=$?
        log "chromium exited with status ${CHROMIUM_STATUS}"
        exit "$CHROMIUM_STATUS"
    fi
    sleep 0.5 2>/dev/null || sleep 1
done
