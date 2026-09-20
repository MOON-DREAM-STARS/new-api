#!/bin/sh
set -eu

# KasmVNC starts Xvnc itself; the X startup hook only has to keep the session
# alive. The runtime entrypoint starts Chromium after the display is ready.
exec sleep infinity