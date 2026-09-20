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
WW_KASM_PORT="${WW_KASM_PORT:-6901}"
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

WW_IME_STATE_DIR="$WW_WORKSPACE_DIR/.guard"
WW_IME_STATE_FILE="$WW_IME_STATE_DIR/ime-state.json"

KASM_PID=""
WEBSOCKIFY_PID=""
CHROMIUM_PID=""
GUARD_PID=""
CLIPD_PID=""

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

write_ime_state() {
    state="$1"
    case "$state" in
        READY|UNAVAILABLE) ;;
        *) return 1 ;;
    esac

    mkdir -p "$WW_IME_STATE_DIR" 2>/dev/null || return 1
    chmod 700 "$WW_IME_STATE_DIR" 2>/dev/null || true

    state_tmp="$WW_IME_STATE_DIR/.ime-state.$$"
    if ! printf '%s' "{\"state\":\"$state\"}" > "$state_tmp"; then
        rm -f "$state_tmp" 2>/dev/null || true
        return 1
    fi
    chmod 600 "$state_tmp" 2>/dev/null || true
    if ! mv -f "$state_tmp" "$WW_IME_STATE_FILE" 2>/dev/null; then
        rm -f "$state_tmp" 2>/dev/null || true
        return 1
    fi
    return 0
}

port_is_listening() {
    port="$1"
    netstat -ltn 2>/dev/null | grep -q ":${port}[[:space:]]"
}

start_kasmvnc() {
    mkdir -p "$HOME/.vnc"
    chmod 700 "$HOME/.vnc" 2>/dev/null || true
    cat > "$HOME/.vnc/kasmvnc.yaml" <<EOF
desktop:
  resolution:
    width: ${WW_SCREEN_WIDTH}
    height: ${WW_SCREEN_HEIGHT}
  allow_resize: false
network:
  protocol: http
  ssl:
    require_ssl: false
    pem_certificate: /usr/local/share/kasmvnc/snakeoil.pem
    pem_key: /usr/local/share/kasmvnc/snakeoil.key
EOF

    # KasmVNC's wrapper requires a password file even when websocket/basic
    # authentication is disabled. The ephemeral credential never leaves the
    # runtime workspace and is not logged.
    KASM_DUMMY_PASSWORD="$(head -c 24 /dev/urandom | base64)"
    printf '%s\n%s\n' "$KASM_DUMMY_PASSWORD" "$KASM_DUMMY_PASSWORD" \
        | kasmvncpasswd -u webworkspace -w "$HOME/.kasmpasswd" >/dev/null
    chmod 600 "$HOME/.kasmpasswd" 2>/dev/null || true
    unset KASM_DUMMY_PASSWORD

    log "starting KasmVNC display=${WW_DISPLAY} rfb_port=${WW_VNC_PORT} websocket_port=${WW_KASM_PORT}"
    vncserver "$WW_DISPLAY" \
        -geometry "${WW_SCREEN_WIDTH}x${WW_SCREEN_HEIGHT}" \
        -depth 24 \
        -rfbport "$WW_VNC_PORT" \
        -noWebsocket \
        -interface 0.0.0.0 \
        -select-de manual \
        -SecurityTypes None \
        -DisableBasicAuth \
        -sslOnly 0 \
        -key /usr/local/share/kasmvnc/snakeoil.key \
        -cert /usr/local/share/kasmvnc/snakeoil.pem \
        -xstartup /usr/local/bin/kasm-xstartup.sh \
        -fg >/tmp/kasmvnc.log 2>&1 &
    KASM_PID=$!

    display_number="${WW_DISPLAY#:}"
    display_number="${display_number%%.*}"
    display_socket="/tmp/.X11-unix/X${display_number}"

    i=0
    while [ ! -S "$display_socket" ]; do
        if ! process_running "$KASM_PID"; then
            log "KasmVNC exited before X display $WW_DISPLAY became ready"
            [ -f /tmp/kasmvnc.log ] && cat /tmp/kasmvnc.log >&2
            return 1
        fi
        i=$((i + 1))
        if [ "$i" -ge 300 ]; then
            log "timed out waiting for KasmVNC display $display_socket"
            [ -f /tmp/kasmvnc.log ] && cat /tmp/kasmvnc.log >&2
            return 1
        fi
        sleep 0.1 2>/dev/null || sleep 1
    done

    i=0
    while ! xdpyinfo -display "$WW_DISPLAY" >/dev/null 2>&1; do
        if ! process_running "$KASM_PID"; then
            log "KasmVNC exited before xdpyinfo succeeded"
            [ -f /tmp/kasmvnc.log ] && cat /tmp/kasmvnc.log >&2
            return 1
        fi
        i=$((i + 1))
        if [ "$i" -ge 300 ]; then
            log "timed out waiting for KasmVNC X server"
            [ -f /tmp/kasmvnc.log ] && cat /tmp/kasmvnc.log >&2
            return 1
        fi
        sleep 0.1 2>/dev/null || sleep 1
    done

    i=0
    while ! port_is_listening "$WW_VNC_PORT"; do
        if ! process_running "$KASM_PID"; then
            log "KasmVNC exited before its RFB port became ready"
            [ -f /tmp/kasmvnc.log ] && cat /tmp/kasmvnc.log >&2
            return 1
        fi
        i=$((i + 1))
        if [ "$i" -ge 300 ]; then
            log "timed out waiting for KasmVNC RFB port ${WW_VNC_PORT}"
            [ -f /tmp/kasmvnc.log ] && cat /tmp/kasmvnc.log >&2
            return 1
        fi
        sleep 0.1 2>/dev/null || sleep 1
    done

    log "KasmVNC ready pid=$KASM_PID rfb_port=$WW_VNC_PORT"
}

start_kasm_web_client() {
    # KasmVNC 1.5.0 binds either its websocket listener or its traditional RFB
    # listener, not both. Keep KasmVNC's X server and RFB on 5900, and publish
    # KasmVNC's own web client assets plus the websocket transport on 6901.
    log "starting KasmVNC web client transport websocket_port=${WW_KASM_PORT} rfb_target=127.0.0.1:${WW_VNC_PORT}"
    websockify --web=/usr/share/kasmvnc/www "${WW_KASM_PORT}" "127.0.0.1:${WW_VNC_PORT}" >/tmp/websockify.log 2>&1 &
    WEBSOCKIFY_PID=$!

    i=0
    while ! port_is_listening "$WW_KASM_PORT"; do
        if ! process_running "$WEBSOCKIFY_PID"; then
            log "websockify exited before port $WW_KASM_PORT became ready"
            [ -f /tmp/websockify.log ] && cat /tmp/websockify.log >&2
            return 1
        fi
        i=$((i + 1))
        if [ "$i" -ge 100 ]; then
            log "timed out waiting for websockify port $WW_KASM_PORT"
            [ -f /tmp/websockify.log ] && cat /tmp/websockify.log >&2
            return 1
        fi
        sleep 0.1 2>/dev/null || sleep 1
    done
    log "websockify ready pid=$WEBSOCKIFY_PID websocket_port=$WW_KASM_PORT"
}

cleanup() {
    status=$?
    trap - EXIT INT TERM

    log "shutting down: workspace-guard -> chromium -> workspace-clipd -> websockify -> KasmVNC"
    terminate_process "$GUARD_PID" workspace-guard
    terminate_process "$CHROMIUM_PID" chromium
    terminate_process "$CLIPD_PID" workspace-clipd
    terminate_process "$WEBSOCKIFY_PID" websockify
    terminate_process "$KASM_PID" Xvnc
    vncserver -kill "$WW_DISPLAY" >/dev/null 2>&1 || true
    log "shutdown complete"

    exit "$status"
}

trap cleanup EXIT INT TERM

log "starting provider=${WW_PROVIDER} workspace=${WW_WORKSPACE_DIR} display=${WW_DISPLAY} screen=${WW_SCREEN_WIDTH}x${WW_SCREEN_HEIGHT} kasm_port=${WW_KASM_PORT} rfb_port=${WW_VNC_PORT} guard_mode=${WW_GUARD_MODE} start_url=${WW_START_URL}"

umask 077
mkdir -p "$WW_WORKSPACE_DIR/profile" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$XDG_DATA_HOME" "$XDG_RUNTIME_DIR" "$WW_WORKSPACE_DIR/tmp" "$WW_IME_STATE_DIR"
chmod 700 "$XDG_RUNTIME_DIR" "$WW_WORKSPACE_DIR/tmp" "$WW_IME_STATE_DIR" 2>/dev/null || true

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

write_ime_state UNAVAILABLE 2>/dev/null || true
if ! start_kasmvnc; then
    write_ime_state UNAVAILABLE 2>/dev/null || log "warning: failed to write UNAVAILABLE to $WW_IME_STATE_FILE"
    log "KasmVNC failed to start: failing closed"
    exit 1
fi
if ! start_kasm_web_client; then
    write_ime_state UNAVAILABLE 2>/dev/null || true
    log "KasmVNC web client transport failed to start: failing closed"
    exit 1
fi
write_ime_state READY 2>/dev/null || log "warning: failed to write READY to $WW_IME_STATE_FILE"

log "starting workspace-clipd socket=$WW_WORKSPACE_DIR/tmp/clipd.sock"
workspace-clipd &
CLIPD_PID=$!

i=0
while [ ! -S "$WW_WORKSPACE_DIR/tmp/clipd.sock" ]; do
    if ! process_running "$CLIPD_PID"; then
        log "workspace-clipd exited before its socket became ready"
        exit 1
    fi
    i=$((i + 1))
    if [ "$i" -ge 100 ]; then
        log "timed out waiting for workspace-clipd socket"
        exit 1
    fi
    sleep 0.1 2>/dev/null || sleep 1
done
log "workspace-clipd ready socket=$WW_WORKSPACE_DIR/tmp/clipd.sock"

log "starting chromium with profile=$WW_WORKSPACE_DIR/profile guard_mode=$WW_GUARD_MODE"
chromium --no-sandbox \
    --test-type \
    --disable-gpu \
    --disable-software-rasterizer \
    --disable-dev-shm-usage \
    --disable-component-update \
    --disable-sync \
    --metrics-recording-only \
    --no-pings \
    --disable-breakpad \
    --force-device-scale-factor=1 \
    --js-flags=--max-old-space-size=256 \
    --password-store=basic \
    --user-data-dir="$WW_WORKSPACE_DIR/profile" \
    --display="$WW_DISPLAY" \
    --no-first-run \
    --no-default-browser-check \
    --disable-features=TranslateUI,PasswordManager,PasswordManagerOnboarding,AutofillServerCommunication,Vulkan \
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
    if ! process_running "$WEBSOCKIFY_PID"; then
        log "websockify exited: failing closed"
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
