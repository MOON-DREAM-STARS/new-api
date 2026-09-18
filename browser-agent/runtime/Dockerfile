FROM alpine:3.20

RUN apk add --no-cache \
      chromium \
      xvfb \
      x11vnc \
      font-noto \
      ca-certificates \
    && rm -rf /var/cache/apk/*

RUN addgroup -g 10001 webworkspace \
    && adduser -D -H -u 10001 -G webworkspace -h /workspace -s /sbin/nologin webworkspace

COPY entrypoint.sh /usr/local/bin/web-workspace-entrypoint.sh
RUN chmod 0755 /usr/local/bin/web-workspace-entrypoint.sh

WORKDIR /workspace
USER webworkspace

ENV HOME=/workspace \
    WW_WORKSPACE_DIR=/workspace \
    WW_DISPLAY=:99 \
    WW_SCREEN_WIDTH=1280 \
    WW_SCREEN_HEIGHT=720 \
    WW_VNC_PORT=5900

ENTRYPOINT ["/usr/local/bin/web-workspace-entrypoint.sh"]