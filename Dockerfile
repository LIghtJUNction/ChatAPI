FROM node:24-alpine AS frontend-build
WORKDIR /src

COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN npm --prefix frontend ci

COPY frontend ./frontend
RUN mkdir -p backend/internal/http/webassets \
    && npm --prefix frontend run build:embedded

FROM golang:1.26-alpine AS backend-build
WORKDIR /src/backend
ARG GOPROXY=https://proxy.golang.org,direct

COPY backend/go.mod backend/go.sum ./
RUN GOPROXY="$GOPROXY" go mod download

COPY backend ./
COPY --from=frontend-build /src/backend/internal/http/webassets/dist ./internal/http/webassets/dist
RUN CGO_ENABLED=0 go build \
      -buildvcs=false \
      -trimpath \
      -tags "embed_frontend nodynamic" \
      -ldflags "-s -w" \
      -o /out/chatapi \
      ./cmd/server \
    && CGO_ENABLED=0 go build \
      -buildvcs=false \
      -trimpath \
      -ldflags "-s -w" \
      -o /out/migrate-db \
      ./cmd/migrate-db

FROM alpine:3.23

RUN apk add --no-cache ca-certificates curl tzdata \
    && addgroup -S chatapi \
    && adduser -S -G chatapi -h /app chatapi \
    && install -d -o chatapi -g chatapi /data

WORKDIR /app
COPY --from=backend-build /out/chatapi /usr/local/bin/chatapi
COPY --from=backend-build /out/migrate-db /usr/local/bin/migrate-db

ENV CHATAPI_HOST=0.0.0.0 \
    CHATAPI_PORT=5000 \
    CHATAPI_DATA_DIR=/data \
    CHATAPI_DB_DRIVER=sqlite \
    CHATAPI_DB_DSN=/data/chatapi.sqlite3 \
    CHATAPI_MEDIA_DERIVED_DIR=/data/derived/request_media \
    CHATAPI_LOG_FORMAT=json \
    CHATAPI_LOG_HTTP_SUMMARY_ENABLED=0

VOLUME ["/data"]
EXPOSE 5000
USER chatapi

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD curl -fsS -o /dev/null http://127.0.0.1:5000/api/ready || exit 1

ENTRYPOINT ["/usr/local/bin/chatapi"]
