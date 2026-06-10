FROM golang:1.25-bookworm AS builder

# Build arguments for version information (passed from CI)
ARG VERSION=dev
ARG BRANCH_NAME=unknown

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    gcc \
    g++ \
    libc6-dev \
    unixodbc-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Copy dependency files 
COPY go.mod go.sum ./

# Download dependencies with cache mount
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY . .

# Generate registry imports
RUN go run ./cmd/genregistry

# Build the application
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=1 go build -v \
    -ldflags="-s -w -X github.com/bruin-data/ingestr/cmd.Version=${VERSION} -X main.commit=${BRANCH_NAME}" \
    -o /src/bin/gong .

FROM debian:bookworm-slim

# Install minimal runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    unixodbc \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user for security
RUN useradd --create-home --shell /bin/bash gong

# Switch to non-root user
USER gong
WORKDIR /home/gong

# Copy the built binary from builder stage
COPY --from=builder /src/bin/gong /usr/local/bin/gong

# Set entrypoint
CMD ["gong"]
