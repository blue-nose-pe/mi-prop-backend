## Multi-stage build de exams_service. Produce DOS imágenes:
##   /out/server   → "exams-service"          (Deployment server, gRPC)
##   /out/migrate  → "exams-service-migrate"  (Job migrate, T-SQL embebido)
##
## El server importa protos generados de keys_service via go.work, así que
## el contexto del build DEBE ser la RAÍZ del repo (no exams_service/).
##
## Build:
##   docker build --target server  -f exams_service/Dockerfile -t exams-service:tag .
##   docker build --target migrate -f exams_service/Dockerfile -t exams-service-migrate:tag .

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

WORKDIR /workspace/exams_service
RUN go mod download

# Protos generados de los hermanos que el server importa (KeysClient gRPC).
WORKDIR /workspace
COPY keys_service/proto keys_service/proto

# Código completo del exams_service.
COPY exams_service/ exams_service/

WORKDIR /workspace/exams_service
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static:nonroot AS server
COPY --from=builder /out/server /server
EXPOSE 50052
USER nonroot:nonroot
ENTRYPOINT ["/server"]

FROM gcr.io/distroless/static:nonroot AS migrate
COPY --from=builder /out/migrate /migrate
USER nonroot:nonroot
ENTRYPOINT ["/migrate"]
