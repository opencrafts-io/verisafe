# --- Stage 1: Builder ---
# Change the base image to use a version of Go >= 1.24.6.
# Using 'golang:1.24-alpine' ensures the correct version is used.
FROM golang:1.24-alpine AS builder

# Set the CGO_ENABLED flag to 0 to ensure the binary is statically linked.
ENV CGO_ENABLED=0

# Set the working directory for the source code inside the container
WORKDIR /src

# Copy the entire source code into the builder's workspace.
COPY . .

# Compile the application. This step will now succeed.
RUN go build -o /app -ldflags "-s -w" main.go # <-- Ensure this path is correct

# --- Stage 2: Final Runtime ---
FROM scratch AS final

# Copy SSL/TLS certificates needed for HTTPS or secure database connections.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the compiled binary named 'app' from the builder stage.
COPY --from=builder /app /app

# Define the entrypoint to run the binary.
ENTRYPOINT ["/app"]
