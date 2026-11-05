# 1. frontend → internal/web/dist
FROM node:lts-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 2. static Go binary with the UI embedded
FROM golang:alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/docket ./cmd/docket

# 3. runtime
FROM gcr.io/distroless/static
COPY --from=build /out/docket /app/docket
COPY seed /app/seed
ENV DOCKET_ADDR=:7777 \
    DOCKET_DB=/data/docket.db \
    DOCKET_BACKUP_DIR=/data/backups
EXPOSE 7777
VOLUME /data
ENTRYPOINT ["/app/docket"]
