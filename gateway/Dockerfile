# gateway — único microservicio expuesto al exterior. Single-binary.
# Importa proto-generated code de los otros services via go.work, así
# que el contexto del build DEBE ser la RAÍZ del repo (no gateway/).
#
# Build:
#   docker build -f gateway/Dockerfile -t gateway:tag .

FROM golang:1.24-alpine AS builder
WORKDIR /workspace
RUN apk add --no-cache git

# go.work + go.mod de cada module — necesarios para resolver los imports
# cross-service. Como los servicios dependientes solo se referencian por
# sus protos generados, no hace falta copiar todo su código fuente.
COPY go.work go.work.sum* ./
COPY users_service/go.mod users_service/go.sum users_service/
COPY exams_service/go.mod exams_service/go.sum exams_service/
COPY keys_service/go.mod keys_service/go.sum keys_service/
COPY satisfaction_service/go.mod satisfaction_service/go.sum satisfaction_service/
COPY hubspot_service/go.mod hubspot_service/go.sum hubspot_service/
COPY analytics_service/go.mod analytics_service/go.sum analytics_service/
COPY gateway/go.mod gateway/go.sum gateway/

WORKDIR /workspace/gateway
RUN go mod download

# Protos generados de los hermanos (lo único que el gateway importa).
WORKDIR /workspace
COPY users_service/proto users_service/proto
COPY exams_service/proto exams_service/proto
COPY keys_service/proto keys_service/proto
COPY satisfaction_service/proto satisfaction_service/proto
COPY hubspot_service/proto hubspot_service/proto
COPY analytics_service/proto analytics_service/proto

# Código completo del gateway.
COPY gateway/ gateway/

WORKDIR /workspace/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/gateway ./cmd/server

FROM gcr.io/distroless/static:nonroot AS server
COPY --from=builder /out/gateway /gateway
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/gateway"]
