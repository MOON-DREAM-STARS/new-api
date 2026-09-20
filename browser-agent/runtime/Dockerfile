# The guard and clipboard helper are built from the browser-agent module so the
# browser layer and the agent share one policy table. The build context is
# browser-agent/ (see runtime/README.md).
FROM golang:1.26.1-alpine AS guard-build

WORKDIR /build
COPY go.mod go.sum ./
COPY internal/policy internal/policy
COPY internal/provider internal/provider
COPY internal/runtime internal/runtime
COPY internal/guard internal/guard
COPY cmd/workspace-guard cmd/workspace-guard
RUN CGO_ENABLED=0 go build -trimpath -o /out/workspace-guard ./cmd/workspace-guard

FROM golang:1.26.1-alpine AS clipd-build

WORKDIR /build
COPY go.mod go.sum ./
COPY cmd/workspace-clipd cmd/workspace-clipd
RUN CGO_ENABLED=0 go build -trimpath -o /out/workspace-clipd ./cmd/workspace-clipd

FROM debian:bookworm-slim

ARG KASMVNC_VERSION=1.5.0

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      ca-certificates \
      chromium \
      curl \
      fonts-dejavu-core \
      fonts-noto-cjk \
      net-tools \
      procps \
      ssl-cert \
      websockify \
      x11-utils \
      xauth \
      xclip \
      xdotool \
    && rm -rf /var/lib/apt/lists/*

RUN curl -L --fail --silent --show-error \
      "https://github.com/kasmtech/KasmVNC/releases/download/v${KASMVNC_VERSION}/kasmvncserver_bookworm_${KASMVNC_VERSION}_amd64.deb" \
      -o /tmp/kasmvncserver.deb \
    && apt-get update \
    && apt-get install -y --no-install-recommends /tmp/kasmvncserver.deb \
    && rm -f /tmp/kasmvncserver.deb \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd --gid 10001 webworkspace \
    && useradd --uid 10001 --gid webworkspace --groups ssl-cert --home-dir /workspace --no-create-home --shell /usr/sbin/nologin webworkspace

# KasmVNC always initialises its websocket TLS material even when HTTP is the
# active protocol. Copy the image-local snakeoil pair to a path readable by the
# runtime user instead of widening /etc/ssl/private.
RUN mkdir -p /usr/local/share/kasmvnc \
    && install -o root -g webworkspace -m 0640 /etc/ssl/private/ssl-cert-snakeoil.key /usr/local/share/kasmvnc/snakeoil.key \
    && install -o root -g webworkspace -m 0644 /etc/ssl/certs/ssl-cert-snakeoil.pem /usr/local/share/kasmvnc/snakeoil.pem

COPY runtime/entrypoint.sh /usr/local/bin/web-workspace-entrypoint.sh
COPY runtime/kasm-xstartup.sh /usr/local/bin/kasm-xstartup.sh
COPY --from=guard-build /out/workspace-guard /usr/local/bin/workspace-guard
COPY --from=clipd-build /out/workspace-clipd /usr/local/bin/workspace-clipd
RUN chmod 0755 /usr/local/bin/web-workspace-entrypoint.sh /usr/local/bin/kasm-xstartup.sh /usr/local/bin/workspace-guard /usr/local/bin/workspace-clipd

WORKDIR /workspace
USER webworkspace:webworkspace

ENV HOME=/workspace \
    WW_WORKSPACE_DIR=/workspace \
    WW_DISPLAY=:99 \
    WW_SCREEN_WIDTH=1280 \
    WW_SCREEN_HEIGHT=720 \
    WW_VNC_PORT=5900 \
    WW_KASM_PORT=6901

ENTRYPOINT ["/usr/local/bin/web-workspace-entrypoint.sh"]
