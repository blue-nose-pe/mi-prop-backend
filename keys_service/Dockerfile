## Multi-stage build de keys_service. Produce DOS imagenes:
##   /out/server   -> "keys-service"          (Deployment server, gRPC)
##   /out/migrate  -> "keys-service-migrate"  (Job migrate, T-SQL embebido)
##
## El server importa protos generados de hubspot_service via go.work (cliente
## SyncKey para replicar custom object Key al CRM). El contexto DEBE ser la
## RAIZ del monorepo (no keys_service/) para que go.work resuelva los modulos
## hermanos.
##
## Build:
##   docker build --target server  -f keys_service/Dockerfile -t keys-service:tag .
##   docker build --target migrate -f keys_service/Dockerfile -t keys-service-migrate:tag .

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

WORKDIR /workspace/keys_service
RUN go mod download

# Protos generados de los hermanos que el server importa (HubspotClient gRPC).
WORKDIR /workspace
COPY hubspot_service/proto hubspot_service/proto

# Codigo completo del keys_service.
COPY keys_service/ keys_service/

WORKDIR /workspace/keys_service
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static:nonroot AS server
COPY --from=builder /out/server /server
EXPOSE 50053
USER nonroot:nonroot
ENTRYPOINT ["/server"]

FROM gcr.io/distroless/static:nonroot AS migrate
COPY --from=builder /out/migrate /migrate
USER nonroot:nonroot
ENTRYPOINT ["/migrate"]
