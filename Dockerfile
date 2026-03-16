# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build deps
RUN apk add --no-cache git

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o wishlist-tracker ./cmd/server

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary and frontend
COPY --from=builder /app/wishlist-tracker .
COPY --from=builder /app/web ./web

# SQLite DB will live on persistent storage
# Azure App Service: /home is persistent (mounted at runtime)
ENV DATABASE_PATH=/home/data/wishlist.db

EXPOSE 8080

# Create data dir at runtime (Azure mounts /home after container starts)
CMD mkdir -p /home/data && ./wishlist-tracker
