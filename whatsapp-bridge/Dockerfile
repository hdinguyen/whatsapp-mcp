FROM golang:1.24.2-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -o whatsapp-bridge .

# Create final image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates sqlite

# Create directory for storing data
RUN mkdir -p /app/store

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/whatsapp-bridge .

# Expose the REST API port
EXPOSE 8080

# Run the application
CMD ["./whatsapp-bridge"]
