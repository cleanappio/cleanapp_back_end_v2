fn main() {
    tonic_build::configure()
        .protoc_arg("--experimental_allow_proto3_optional")
        .compile_protos(
            &[
                "proto/stxn.proto",
            ],
            &["proto"],
        )
        .expect("Failed to compile stxn proto");
} 