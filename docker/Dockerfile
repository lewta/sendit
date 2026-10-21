# syntax=docker/dockerfile:1

# ── builder ──────────────────────────────────────────────────────────────────
FROM golang:1.26.10-alpine3.24@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /sendit ./cmd/sendit

# ── runtime ──────────────────────────────────────────────────────────────────
FROM alpine:3.21@sha256:c3f8e73fdb79deaebaa2037150150191b9dcbfba68b4a46d70103204c53f4709

# ca-certificates: needed for HTTPS targets
# tzdata: correct timestamps in logs
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /sendit /usr/local/bin/sendit

# Config is mounted at runtime; provide a placeholder so the binary can
# start without a volume when no config is supplied.
RUN mkdir -p /etc/sendit

EXPOSE 9090

ENTRYPOINT ["sendit", "start", "--foreground", "--config", "/etc/sendit/config.yaml"]
