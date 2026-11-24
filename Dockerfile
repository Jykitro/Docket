# 1. frontend → internal/web/dist
FROM --platform=$BUILDPLATFORM oven/bun:1.4.5-alpine AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock web/bunfig.toml ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# 2. static Go binary with the UI embedded
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY skills ./skills
COPY --from=web /src/internal/web/dist ./internal/web/dist
ARG TARGETOS
ARG TARGETARCH
RUN mkdir /out-socket /out-data && chmod 0700 /out-data
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/plandillo ./cmd/docket && ln -s plandillo /out/docket

# 3. runtime
FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
COPY --from=build --chown=65532:65532 /out-data /data
COPY --from=build --chown=65532:65532 /out-socket /run/docket-code
COPY --from=build /out/ /app/
ENV DOCKET_ADDR=:7777 \
    DOCKET_DB=/data/docket.db \
    DOCKET_BACKUP_DIR=/data/backups
USER 65532:65532
EXPOSE 7777
VOLUME /data
ENTRYPOINT ["/app/plandillo"]
