# Single-binary build de analytics_service. Sin DB → sin migrate.
FROM golang:1.24-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server

FROM gcr.io/distroless/static:nonroot AS server
COPY --from=builder /out/server /server
EXPOSE 50056
USER nonroot:nonroot
ENTRYPOINT ["/server"]
