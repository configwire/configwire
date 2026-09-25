# ConfigWire server image (PocketBase-based, single binary + ./pb_public).
# Build context MUST be configwire/ (this dir), so ./pb_public resolves:
#   docker build -t configwire ./configwire
# Run:
#   docker run --rm -p 8090:8090 -v cw-data:/app/pb_data configwire
# Or: docker compose up --build
FROM golang:1.27-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
# VERSION may be overridden at build time (--build-arg VERSION=vX.Y.Z);
# falls back to the embedded VERSION file when left as dev.
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.Version=$VERSION" -o /out/configwire .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/configwire ./
COPY pb_public ./pb_public
VOLUME ["/app/pb_data"]
EXPOSE 8090
# NOTE: 0.0.0.0 (not 127.0.0.1) so the port is reachable outside the container.
# Data dir defaults to ./pb_data locally; in-container it is /app/pb_data.
ENTRYPOINT ["./configwire", "serve", "--http=0.0.0.0:8090", "--dir=/app/pb_data"]
