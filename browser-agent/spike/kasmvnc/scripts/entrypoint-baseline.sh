#!/bin/sh
set -eu

WIDTH="${WW_SCREEN_WIDTH:-1280}"
HEIGHT="${WW_SCREEN_HEIGHT:-720}"
DISPLAY_NUM="${DISPLAY:-:99}"
PAGE_URL="${SPIKE_PAGE_URL:-file:///opt/spike/page/index.html}"

log() { printf '%s\n' "[spike-baseline] $*"; }

mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$XDG_DATA_HOME" "$XDG_RUNTIME_DIR"
chmod 700 "$XDG_RUNTIME_DIR" 2>/dev/null || true
mkdir -p /tmp/.X11-unix
chmod 1777 /tmp/.X11-unix 2>/dev/null || true

log "starting Xvfb ${WIDTH}x${HEIGHT} display=${DISPLAY_NUM}"
Xvfb "$DISPLAY_NUM" -screen 0 "${WIDTH}x${HEIGHT}x24" -nolisten tcp -ac &
XVFB_PID=$!

display_number="${DISPLAY_NUM#:}"
display_number="${display_number%%.*}"
socket="/tmp/.X11-unix/X${display_number}"
i=0
while [ ! -S "$socket" ]; do
    kill -0 "$XVFB_PID" 2>/dev/null || { log "Xvfb exited"; exit 1; }
    i=$((i + 1)); [ "$i" -lt 100 ] || { log "Xvfb timeout"; exit 1; }
    sleep 0.1
done

log "starting x11vnc with production flags"
x11vnc -display "$DISPLAY_NUM" -rfbport 5900 -forever -shared -nopw -nolookup \
    -deferupdate 50 -wait 30 -quiet -bg -o /tmp/x11vnc.log
X11VNC_PID="$(pidof x11vnc | awk '{print $1}')"

log "starting websockify/noVNC on 6080"
websockify --web=/usr/share/novnc 6080 127.0.0.1:5900 >/tmp/websockify.log 2>&1 &
WEBSOCKIFY_PID=$!

log "starting Chromium"
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
    --user-data-dir="$HOME/profile" \
    --display="$DISPLAY_NUM" \
    --no-first-run \
    --no-default-browser-check \
    --disable-features=TranslateUI,PasswordManager,PasswordManagerOnboarding,AutofillServerCommunication,Vulkan \
    --window-size="${WIDTH},${HEIGHT}" \
    --window-position=0,0 \
    --disable-quic \
    --deny-permission-prompts \
    --app="$PAGE_URL" &
CHROMIUM_PID=$!

cleanup() {
    trap - EXIT INT TERM
    kill -TERM "$CHROMIUM_PID" "$WEBSOCKIFY_PID" "$X11VNC_PID" "$XVFB_PID" 2>/dev/null || true
    wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

log "ready pids chromium=$CHROMIUM_PID x11vnc=$X11VNC_PID websockify=$WEBSOCKIFY_PID"
wait "$CHROMIUM_PID"
