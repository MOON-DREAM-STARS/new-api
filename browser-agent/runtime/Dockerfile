# The guard binary is built from the browser-agent module so that the browser
# layer and the agent share one policy table. The build context is
# browser-agent/ (see runtime/README.md).
FROM golang:1.26.1-alpine AS guard-build

WORKDIR /build
COPY go.mod go.sum ./
COPY internal/policy internal/policy
COPY internal/guard internal/guard
COPY cmd/workspace-guard cmd/workspace-guard
RUN CGO_ENABLED=0 go build -trimpath -o /out/workspace-guard ./cmd/workspace-guard

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

COPY runtime/entrypoint.sh /usr/local/bin/web-workspace-entrypoint.sh
COPY --from=guard-build /out/workspace-guard /usr/local/bin/workspace-guard
RUN chmod 0755 /usr/local/bin/web-workspace-entrypoint.sh /usr/local/bin/workspace-guard

WORKDIR /workspace
USER webworkspace

ENV HOME=/workspace \
    WW_WORKSPACE_DIR=/workspace \
    WW_DISPLAY=:99 \
    WW_SCREEN_WIDTH=1280 \
    WW_SCREEN_HEIGHT=720 \
    WW_VNC_PORT=5900

ENTRYPOINT ["/usr/local/bin/web-workspace-entrypoint.sh"]
