# syntax=docker/dockerfile:1

# --- Web UI ---
FROM --platform=$BUILDPLATFORM node:26-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- Go binary (pure Go, cross-compiled for the target platform) ---
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/internal/ui/dist internal/ui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/s3freeze ./cmd/s3freeze \
 && mkdir -p /out/data /out/backups

# --- Runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/s3freeze /s3freeze
COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY --from=build --chown=nonroot:nonroot /out/backups /backups
ENV DATA_DIR=/data \
    BACKUP_DIR=/backups \
    PORT=8080
EXPOSE 8080
VOLUME ["/data", "/backups"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/s3freeze", "healthcheck"]
ENTRYPOINT ["/s3freeze"]
