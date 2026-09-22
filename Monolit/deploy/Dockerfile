# syntax=docker/dockerfile:1
FROM golang:1.25.7-bookworm AS builder

WORKDIR /src

# Module and compiler caches live in BuildKit cache mounts instead of image
# layers: the image stays slim and every rebuild reuses the previous compile
# instead of leaving another full snapshot behind in the build cache.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/verbatrace ./cmd

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl ffmpeg tzdata fonts-dejavu-core \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd --system verbatrace \
    && useradd --system --gid verbatrace --home-dir /app --shell /usr/sbin/nologin verbatrace

WORKDIR /app

COPY --from=builder /out/verbatrace /app/verbatrace
COPY migrations /app/migrations

RUN mkdir -p /app/uploads \
    && chown -R verbatrace:verbatrace /app

USER verbatrace

EXPOSE 8080

CMD ["/app/verbatrace"]
