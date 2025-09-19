FROM rust:1.80 as builder
WORKDIR /app
COPY Cargo.toml Cargo.lock ./
RUN mkdir -p src && echo 'fn main(){}' > src/main.rs
RUN cargo build --release || true
COPY . .
RUN cargo build --release

FROM gcr.io/distroless/cc
WORKDIR /
COPY --from=builder /app/target/release/email-fettcher /email-fettcher
ENV RUST_LOG=info
ENTRYPOINT ["/email-fettcher"]

