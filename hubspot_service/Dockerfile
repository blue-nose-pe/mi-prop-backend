# Multi-stage build de hubspot_service (Go). Produce DOS imágenes:
#   /out/server  → "hubspot-service"        (Deployment server: gRPC + HTTP webhook)
#   /out/worker  → "hubspot-service-worker" (Deployment worker: consume asynq queue)
#
# Build con BuildKit:
#   docker build --target server -t hubspot-service:0.1.0 .
#   docker build --target worker -t hubspot-service-worker:0.1.0 .

FROM golang:1.23-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/worker ./cmd/worker

FROM gcr.io/distroless/static:nonroot AS server
COPY --from=builder /out/server /server
EXPOSE 50054 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]

FROM gcr.io/distroless/static:nonroot AS worker
COPY --from=builder /out/worker /worker
USER nonroot:nonroot
ENTRYPOINT ["/worker"]
