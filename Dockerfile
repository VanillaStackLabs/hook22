FROM golang:1.24-alpine AS builder

WORKDIR /app

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build statically linked binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o sftp-webhook-gateway .

# Alpine Runtime
FROM alpine:latest

# Add root certificates for HTTPS (Webhooks, S3, GCS, Azure)
RUN apk --no-cache add ca-certificates

WORKDIR /

# Copy executable from builder
COPY --from=builder /app/sftp-webhook-gateway /sftp-webhook-gateway

# Expose SFTP port (2222) and SSE Log/Metrics API port (8080)
EXPOSE 2222 8080

ENTRYPOINT ["/sftp-webhook-gateway"]