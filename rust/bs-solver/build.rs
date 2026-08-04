//! Generates the gRPC bindings from the shared contract.
//!
//! Generated into `OUT_DIR` rather than committed, unlike the Go side. Go's
//! tooling expects checked-in stubs and Cargo's does not, and following each
//! ecosystem's convention beats imposing one on both — the contract in
//! proto/ is the single source either way.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto_root = std::path::Path::new("../../proto");

    tonic_build::configure()
        // The server implements the service; nothing in this crate calls it.
        // Generating a client anyway would be dead code that still has to
        // compile and still shows up in the dependency graph.
        .build_client(false)
        .compile_protos(
            &[
                proto_root.join("blackstart/solver/v1/solver.proto"),
                proto_root.join("blackstart/weather/v1/grid.proto"),
            ],
            &[proto_root],
        )?;

    // Rebuild when the contract changes. Without this, editing the proto
    // leaves a stale binding in place and the mismatch surfaces somewhere far
    // from its cause.
    println!("cargo:rerun-if-changed=../../proto/blackstart/solver/v1/solver.proto");
    println!("cargo:rerun-if-changed=../../proto/blackstart/weather/v1/grid.proto");

    Ok(())
}
