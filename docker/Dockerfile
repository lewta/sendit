# syntax=docker/dockerfile:1

# ── builder ──────────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /sendit ./cmd/sendit

# ── runtime ──────────────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates: needed for HTTPS targets
# tzdata: correct timestamps in logs
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /sendit /usr/local/bin/sendit

# Config is mounted at runtime; provide a placeholder so the binary can
# start without a volume when no config is supplied.
RUN mkdir -p /etc/sendit

EXPOSE 9090

ENTRYPOINT ["sendit", "start", "--foreground", "--config", "/etc/sendit/config.yaml"]
