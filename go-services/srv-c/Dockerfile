# Use the official Golang image as a parent image
FROM golang:latest as builder

# Set the working directory
WORKDIR /app

# Copy the Go Modules manifests
COPY go.mod go.sum ./

# Download the Go module dependencies
RUN go mod download

# Copy the source code into the container
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -v -o serviceC

# Use a smaller image to run the application
FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/serviceC .


# Expose port 8080
EXPOSE 8080

# Run the application
CMD ["./serviceC"]
