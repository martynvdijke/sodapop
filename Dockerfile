# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
WORKDIR /src
ARG VERSION=0.0.0-dev
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o /out/sodapop ./cmd/server \
    && go build -trimpath -ldflags="-s -w" -o /out/sodapop-seed ./cmd/seed

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S sodapop \
    && adduser -S -G sodapop -h /data sodapop
WORKDIR /app
COPY --from=build /out/sodapop /usr/local/bin/sodapop
COPY --from=build /out/sodapop-seed /usr/local/bin/sodapop-seed
RUN mkdir -p /data && chown -R sodapop:sodapop /data
USER sodapop
ENV ENV=production \
    PORT=8097 \
    DATA_DIR=/data \
    DATABASE_PATH=/data/sodapop.db
VOLUME ["/data"]
EXPOSE 8097
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8097/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/sodapop"]
