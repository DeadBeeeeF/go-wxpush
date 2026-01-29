# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o go-wxpush .

# Run stage
FROM alpine:latest

WORKDIR /app

# Install certificates and timezone data
RUN apk --no-cache add ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/go-wxpush .

# Copy config.yml if it exists (optional, mostly for local dev mapping)
# COPY config.yml . 

EXPOSE 5566

ENTRYPOINT ["./go-wxpush"]