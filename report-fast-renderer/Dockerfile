FROM rust:1.85-bullseye AS builder

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy Cargo files first for better caching
COPY Cargo.toml ./

# Copy source code
COPY . .

# Build the application
RUN cargo install --path . -j $(nproc)

# Runtime stage
FROM debian:bullseye-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user for security
RUN groupadd -r renderer && useradd -r -g renderer renderer

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /usr/local/cargo/bin/report-fast-renderer /usr/local/bin/report-fast-renderer

# Change ownership to non-root user
RUN chown -R renderer:renderer /app

# Switch to non-root user
USER renderer

# Expose the port the app runs on
EXPOSE 8080

# Set environment variables
ENV RUST_LOG=info
ENV RUST_BACKTRACE=1

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Run the application
CMD ["report-fast-renderer"]
