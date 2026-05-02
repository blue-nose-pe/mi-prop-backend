# Multi-stage build: satisfaction-service + satisfaction-service-migrate.

FROM golang:1.23-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static:nonroot AS server
COPY --from=builder /out/server /server
EXPOSE 50055
USER nonroot:nonroot
ENTRYPOINT ["/server"]

FROM gcr.io/distroless/static:nonroot AS migrate
COPY --from=builder /out/migrate /migrate
USER nonroot:nonroot
ENTRYPOINT ["/migrate"]
