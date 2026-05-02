# Multi-stage build que produce DOS binarios:
#   /out/server   → imagen "users-service"           (Deployment)
#   /out/migrate  → imagen "users-service-migrate"   (k8s Job, helm hook)
#
# Las migraciones T-SQL se embeben dentro de /out/migrate vía go:embed,
# así que el binario es self-contained y el Job no necesita montar archivos.
#
# Para construir solo una de las dos imágenes, usar BuildKit con --target:
#   docker build --target server  -t users-service:0.1.0 .
#   docker build --target migrate -t users-service-migrate:0.1.0 .

# ---------- build ----------
FROM golang:1.23-alpine AS builder
WORKDIR /app

# Instala git por si alguna dependencia indirecta lo necesita en go mod.
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static binaries (CGO_ENABLED=0) para correr en distroless.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/migrate ./cmd/migrate

# ---------- runtime: server ----------
FROM gcr.io/distroless/static:nonroot AS server
COPY --from=builder /out/server /server
EXPOSE 50051
USER nonroot:nonroot
ENTRYPOINT ["/server"]

# ---------- runtime: migrate ----------
FROM gcr.io/distroless/static:nonroot AS migrate
COPY --from=builder /out/migrate /migrate
USER nonroot:nonroot
ENTRYPOINT ["/migrate"]
