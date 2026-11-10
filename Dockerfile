# ConfigWire server image (PocketBase-based, single binary + ./pb_public).
# Build context MUST be configwire/ (this dir), so ./pb_public resolves:
#   docker build -t configwire ./configwire
# Run (non-root UID 10001, read-only rootfs, writable data volume):
#   docker run --rm -p 8090:8090 --read-only --tmpfs /tmp --cap-drop ALL \
#     --security-opt no-new-privileges -v cw-data:/app/pb_data configwire
# Or: docker compose up --build
#
# The process writes only under /app/pb_data (volume) and /tmp (tmpfs), so a
# read-only root filesystem works. Bind mounts must be owned by 10001:10001.
#
# GO_TAGS defaults to no_ui, which strips the PocketBase admin UI at /_/
# via upstream's built-in no_ui tag: ganigeorgiev.com/pocketbase/pocketbase/ui/embed_no_ui.go
# leaves ui.DistDirFS nil so /_/ routes never register. Our Admin UI at /
# (./pb_public) and all /api/* routes are unaffected — still copied below.
# Override with --build-arg GO_TAGS= to ship the dashboard.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS builder
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG GO_TAGS="no_ui"
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . ./
# Cross-compile natively on $BUILDPLATFORM: no QEMU emulation even when
# TARGETARCH != BUILDARCH. Go build cache persists across GHA rebuilds via
# the cache mounts, so only changed packages recompile.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath ${GO_TAGS:+-tags=${GO_TAGS}} -o /out/configwire .

# Alpine 3.24 is the current supported minor; pinned by index digest so a
# tag move cannot silently change the runtime base.
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache ca-certificates
RUN addgroup -S -g 10001 configwire \
    && adduser -S -D -H -u 10001 -G configwire configwire
WORKDIR /app
RUN mkdir -p /app/pb_data && chown -R 10001:10001 /app
COPY --chown=10001:10001 --from=builder /out/configwire ./
COPY --chown=10001:10001 pb_public ./pb_public
VOLUME ["/app/pb_data"]
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget --no-verbose --tries=1 --spider http://localhost:8090/healthz || exit 1
USER 10001:10001
# NOTE: 0.0.0.0 (not 127.0.0.1) so the port is reachable outside the container.
# Data dir defaults to ./pb_data locally; in-container it is /app/pb_data.
ENTRYPOINT ["./configwire", "serve", "--http=0.0.0.0:8090", "--dir=/app/pb_data"]
