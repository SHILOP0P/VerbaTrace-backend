FROM golang:1.25.7-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/verbatrace ./cmd

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl ffmpeg tzdata \
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
