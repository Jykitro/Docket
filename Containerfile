FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/code-worker cmd/code-worker
COPY cmd/code-guest cmd/code-guest
COPY internal/codeeval internal/codeeval
COPY internal/toolcatalog internal/toolcatalog
COPY internal/vercelrun internal/vercelrun
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm go build -trimpath -ldflags="-s -w" -o /out/eval.wasm ./cmd/code-guest
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/code-worker

FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
COPY --from=build /out /app
EXPOSE 80
ENTRYPOINT ["/app/worker"]
