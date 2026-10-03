# Production Multi-Stage Dockerfile for TermChat Relay Server
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build statically linked binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o relay-server ./cmd/server

# Minimal runtime image
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/relay-server /relay-server

# Default relay server port
EXPOSE 8080

ENTRYPOINT ["/relay-server", "-addr", ":8080"]
