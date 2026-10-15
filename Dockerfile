# ConfigWire server image (PocketBase-based, single binary + ./pb_public).
# Build context MUST be configwire/ (this dir), so ./pb_public resolves:
#   docker build -t configwire ./configwire
# Run:
#   docker run --rm -p 8090:8090 -v cw-data:/app/pb_data configwire
# Or: docker compose up --build
# Or: docker build --build-arg GO_TAGS=no_ui -t configwire ./configwire
# (strips the PocketBase admin UI at /_/ via upstream's built-in no_ui
# tag: github.com/pocketbase/pocketbase/ui/embed_no_ui.go leaves
# ui.DistDirFS nil so /_/ routes never register. Our Admin UI at /
# (./pb_public) and all /api/* routes are unaffected — still copied below.)
FROM golang:1.27.2-alpine AS builder
ARG GO_TAGS=""
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -trimpath ${GO_TAGS:+-tags=${GO_TAGS}} -o /out/configwire .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/configwire ./
COPY pb_public ./pb_public
VOLUME ["/app/pb_data"]
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget --no-verbose --tries=1 --spider http://localhost:8090/healthz || exit 1
# NOTE: 0.0.0.0 (not 127.0.0.1) so the port is reachable outside the container.
# Data dir defaults to ./pb_data locally; in-container it is /app/pb_data.
ENTRYPOINT ["./configwire", "serve", "--http=0.0.0.0:8090", "--dir=/app/pb_data"]
