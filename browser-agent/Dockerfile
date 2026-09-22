FROM golang:1.26.1-alpine AS build

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY internal internal
COPY cmd cmd
RUN CGO_ENABLED=0 go build -trimpath -o /out/browser-agent ./cmd/browser-agent

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=build /out/browser-agent /usr/local/bin/browser-agent

ENTRYPOINT ["/usr/local/bin/browser-agent"]
