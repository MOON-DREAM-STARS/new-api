#!/bin/sh
set -eu
# KasmVNC already provides the X server. Keep the Xstartup shell alive so the
# wrapper does not treat session startup as completed immediately.
exec sleep infinity
