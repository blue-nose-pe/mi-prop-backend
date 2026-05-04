## Build de analytics_service. Sin DB → sin migrate.
##
## El server importa protos generados de users_service, exams_service y
## keys_service vía go.work, así que el contexto del build DEBE ser la
## RAÍZ del repo (no analytics_service/).
##
## Build:
##   docker build --target server -f analytics_service/Dockerfile -t analytics-service:tag .

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

WORKDIR /workspace/analytics_service
RUN go mod download

# Protos generados de los hermanos que el server importa (UsersClient,
# ExamsClient, KeysClient gRPC).
WORKDIR /workspace
COPY users_service/proto users_service/proto
COPY exams_service/proto exams_service/proto
COPY keys_service/proto keys_service/proto

# Código completo de analytics_service.
COPY analytics_service/ analytics_service/

WORKDIR /workspace/analytics_service
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server

FROM gcr.io/distroless/static:nonroot AS server
COPY --from=builder /out/server /server
EXPOSE 50056
USER nonroot:nonroot
ENTRYPOINT ["/server"]
