## Multi-stage build de users_service. Produce TRES imágenes:
##   /out/server     → "users-service"            (Deployment)
##   /out/migrate    → "users-service-migrate"    (Job pre-install/upgrade)
##   /out/bootstrap  → "users-service-bootstrap"  (Job post-install)
##
## Las migraciones T-SQL se embeben en /out/migrate vía go:embed.
##
## El server importa protos generados de hubspot_service (cliente OTP),
## así que el contexto del build DEBE ser la RAÍZ del repo (no users_service/).
##
## Build:
##   docker build --target server    -f users_service/Dockerfile -t users-service:tag .
##   docker build --target migrate   -f users_service/Dockerfile -t users-service-migrate:tag .
##   docker build --target bootstrap -f users_service/Dockerfile -t users-service-bootstrap:tag .

FROM golang:1.24-alpine AS builder
WORKDIR /workspace
RUN apk add --no-cache git

COPY go.work go.work.sum* ./
COPY users_service/go.mod users_service/go.sum users_service/
COPY exams_service/go.mod exams_service/go.sum exams_service/
COPY keys_service/go.mod keys_service/go.sum keys_service/
COPY satisfaction_service/go.mod satisfaction_service/go.sum satisfaction_service/
COPY hubspot_service/go.mod hubspot_service/go.sum hubspot_service/
COPY analytics_service/go.mod analytics_service/go.sum analytics_service/
COPY gateway/go.mod gateway/go.sum gateway/

WORKDIR /workspace/users_service
RUN go mod download

# Protos generados de los hermanos que el server importa.
WORKDIR /workspace
COPY hubspot_service/proto hubspot_service/proto

# Código completo del users_service.
COPY users_service/ users_service/

WORKDIR /workspace/users_service
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/migrate ./cmd/migrate
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/bootstrap ./cmd/bootstrap

FROM gcr.io/distroless/static:nonroot AS server
COPY --from=builder /out/server /server
EXPOSE 50051
USER nonroot:nonroot
ENTRYPOINT ["/server"]

FROM gcr.io/distroless/static:nonroot AS migrate
COPY --from=builder /out/migrate /migrate
USER nonroot:nonroot
ENTRYPOINT ["/migrate"]

FROM gcr.io/distroless/static:nonroot AS bootstrap
COPY --from=builder /out/bootstrap /bootstrap
USER nonroot:nonroot
ENTRYPOINT ["/bootstrap"]
