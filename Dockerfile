# Build stage
FROM golang:1.22-alpine AS builder

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
# Azure App Service: /home is persistent
# Fly.io: /data is a mounted volume
ENV DATABASE_PATH=/home/data/wishlist.db

# Ensure the data directory exists
RUN mkdir -p /home/data

EXPOSE 8080

CMD ["./wishlist-tracker"]
