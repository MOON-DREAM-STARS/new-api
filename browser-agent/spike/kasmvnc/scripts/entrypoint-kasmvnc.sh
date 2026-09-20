#!/bin/sh
set -eu

WIDTH="${WW_SCREEN_WIDTH:-1280}"
HEIGHT="${WW_SCREEN_HEIGHT:-720}"
DISPLAY_NUM="${DISPLAY:-:99}"
PAGE_URL="${SPIKE_PAGE_URL:-file:///opt/spike/page/index.html}"

log() { printf '%s\n' "[spike-kasmvnc] $*"; }

mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$XDG_DATA_HOME" "$XDG_RUNTIME_DIR"
chmod 700 "$XDG_RUNTIME_DIR" 2>/dev/null || true
mkdir -p "$HOME/.vnc"
printf 'desktop:\n  resolution:\n    width: %s\n    height: %s\n  allow_resize: false\n' "$WIDTH" "$HEIGHT" > "$HOME/.vnc/kasmvnc.yaml"
# KasmVNC's wrapper insists on at least one user even when websocket basic
# authentication is disabled. Generate a throwaway local-only credential; it
# stays in the container's ephemeral HOME and is never written to this repo.
KASM_DUMMY_PASSWORD="$(head -c 24 /dev/urandom | base64)"
printf '%s\n%s\n' "$KASM_DUMMY_PASSWORD" "$KASM_DUMMY_PASSWORD" \
    | kasmvncpasswd -u spike -w "$HOME/.kasmpasswd" >/dev/null

log "starting KasmVNC ${WIDTH}x${HEIGHT} on display=${DISPLAY_NUM} websocket=3000"
vncserver "$DISPLAY_NUM" \
    -geometry "${WIDTH}x${HEIGHT}" \
    -depth 24 \
    -websocketPort 3000 \
    -interface 0.0.0.0 \
    -select-de manual \
    -SecurityTypes None \
    -DisableBasicAuth \
    -xstartup /opt/spike/scripts/kasm-xstartup.sh \
    -fg >/tmp/kasmvnc.log 2>&1 &
KASM_PID=$!

i=0
while ! netstat -ltn 2>/dev/null | grep -q ':3000[[:space:]]'; do
    kill -0 "$KASM_PID" 2>/dev/null || { log "KasmVNC exited"; cat /tmp/kasmvnc.log >&2; exit 1; }
    i=$((i + 1)); [ "$i" -lt 150 ] || { log "KasmVNC timeout"; cat /tmp/kasmvnc.log >&2; exit 1; }
    sleep 0.1
done

log "waiting for KasmVNC X server"
i=0
while ! xdpyinfo -display "$DISPLAY_NUM" >/dev/null 2>&1; do
    kill -0 "$KASM_PID" 2>/dev/null || { log "KasmVNC exited before X server became ready"; cat /tmp/kasmvnc.log >&2; exit 1; }
    i=$((i + 1)); [ "$i" -lt 150 ] || { log "KasmVNC X server timeout"; cat /tmp/kasmvnc.log >&2; exit 1; }
    sleep 0.1
done

log "starting Chromium on KasmVNC display"
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
    kill -TERM "$CHROMIUM_PID" "$KASM_PID" 2>/dev/null || true
    vncserver -kill "$DISPLAY_NUM" >/dev/null 2>&1 || true
    wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

log "ready pids chromium=$CHROMIUM_PID kasmvnc=$KASM_PID"
wait "$CHROMIUM_PID"
