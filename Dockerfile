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
    -o /src/bin/ingestr .

FROM debian:bookworm-slim

# Install minimal runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user for security
RUN useradd --create-home --shell /bin/bash ingestr

# Switch to non-root user
USER ingestr
WORKDIR /home/ingestr

# Copy the built binary from builder stage
COPY --from=builder /src/bin/ingestr /usr/local/bin/ingestr

# Set entrypoint
CMD ["ingestr"]
