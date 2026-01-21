# Stage 1: Base & Build
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Development stage (for hot reloading)
FROM builder AS development
RUN go install github.com/air-verse/air@latest
COPY . .
CMD ["air", "-c", ".air.toml"]

# Production build stage
FROM builder AS build-prod
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o /lingofetch-api ./cmd/api

# Stage 2: Runtime (Production)
FROM alpine:latest AS production

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from build-prod
COPY --from=build-prod /lingofetch-api .

# Copy credentials (for local dev accessibility)
COPY lingofetch-dev-gcp-credentials.json /app/credentials.json

# Expose port
EXPOSE 8080

# Set environment
ENV PORT=8080
ENV ENVIRONMENT=production

# Run the application
CMD ["/app/lingofetch-api"]

