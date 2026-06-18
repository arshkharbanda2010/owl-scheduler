# =============================================================================
# Multi-stage Dockerfile for owl-scheduler
# Stage 1: Build the scheduler binary
# Stage 2: Copy into a minimal distroless image
# =============================================================================

# ---- Stage 1: Builder ----
FROM golang:1.22-alpine AS builder

# Install git and ca-certificates for fetching dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Create a non-root build user
RUN adduser -D -g '' appuser

WORKDIR /workspace

# Copy dependency manifests first (leverage Docker layer caching)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy the full source tree
COPY . .

# Build the scheduler binary with static linking and stripped symbols
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o /workspace/bin/scheduler \
    ./cmd/scheduler/main.go

# ---- Stage 2: Runtime ----
FROM gcr.io/distroless/static-debian12:nonroot

# Copy the statically-compiled binary from the builder stage
COPY --from=builder /workspace/bin/scheduler /scheduler

# Copy the non-root user from the builder
COPY --from=builder /etc/passwd /etc/passwd

# Use the distroless "nonroot" user (uid 65532)
USER nonroot:nonroot

# Expose the health / metrics port (can be overridden at runtime)
EXPOSE 10259

# Expose the metrics port
EXPOSE 9443

# Run the scheduler
ENTRYPOINT ["/scheduler"]
