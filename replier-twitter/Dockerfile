FROM rust:1.88-bullseye AS builder

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
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /usr/local/cargo/bin/replier-twitter /usr/local/bin/replier-twitter

# Set environment variables
ENV RUST_LOG=info
ENV RUST_BACKTRACE=1

# Run the application
CMD ["replier-twitter"]

