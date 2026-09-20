#!/bin/sh
set -eu

workspace_dir="${WW_WORKSPACE_DIR:-/workspace}"
socket_path="$workspace_dir/tmp/clipd.sock"
state_path="$workspace_dir/.guard/ime-state.json"

fail() {
    printf '%s\n' "smoke-kasmvnc-clipd: FAIL: $*" >&2
    exit 1
}

find_pid() {
    pid="$(pgrep -a "$1" 2>/dev/null | sed -n '1p' | awk '{print $1}')" || true
    [ -n "$pid" ] || return 1
    printf '%s\n' "$pid"
}

for command_name in workspace-clipd xclip xdotool Xvnc websockify chromium; do
    command -v "$command_name" >/dev/null 2>&1 || fail "missing command: $command_name"
done

[ -S "$socket_path" ] || fail "clipd socket is missing: $socket_path"
[ "$(stat -c '%a' "$socket_path")" = "600" ] || fail "clipd socket mode is not 600"
[ "$(stat -c '%a' "$workspace_dir/tmp")" = "700" ] || fail "workspace tmp directory mode is not 700"
[ -f "$state_path" ] || fail "ime state file is missing: $state_path"
[ "$(stat -c '%a' "$workspace_dir/.guard")" = "700" ] || fail "guard state directory mode is not 700"

state="$(cat "$state_path")"
case "$state" in
    '{"state":"READY"}'|'{"state":"UNAVAILABLE"}') ;;
    *) fail "unexpected ime state: $state" ;;
esac

[ -n "$(find_pid workspace-clipd)" ] || fail "workspace-clipd is not running"
[ -n "$(find_pid Xvnc)" ] || fail "KasmVNC Xvnc is not running"
[ -n "$(find_pid websockify)" ] || fail "websockify is not running"
[ -n "$(find_pid chromium)" ] || fail "chromium is not running"

if [ "$state" = '{"state":"READY"}' ]; then
    [ -n "$(find_pid Xvnc)" ] || fail "KasmVNC Xvnc is not running"
    [ -n "$(find_pid websockify)" ] || fail "websockify is not running"
fi
if find_pid ibus-daemon >/dev/null 2>&1; then fail "unexpected ibus-daemon process"; fi
if find_pid dbus-daemon >/dev/null 2>&1; then fail "unexpected dbus-daemon process"; fi


printf '%s\n' "smoke-kasmvnc-clipd: PASS state=$state socket=$socket_path"