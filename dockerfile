# Start from a Golang base image
FROM golang:1.23-alpine

# Install FFmpeg and other necessary tools
RUN apk add --no-cache ffmpeg

# Set the working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code into the container
COPY . .

# Build the application
RUN go build -o main .

# Expose the port the app runs on
EXPOSE 3000

# Set environment variables
ENV REDIS_ADDR=10.10.79.15:6379
ENV CACHE_DIR=/app/cache

# Run the binary
CMD ["./main"]