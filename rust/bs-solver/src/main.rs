//! The solver server.

use std::net::SocketAddr;

use bs_solver::Solver;
use bs_solver::proto::solver_service_server::SolverServiceServer;
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Loopback by default. The solver holds no credentials and enforces no
    // authorization, so it is a component of the platform rather than a
    // service to expose — binding 0.0.0.0 by default would make that mistake
    // the easy one.
    let address: SocketAddr = std::env::var("BLACKSTART_SOLVER_ADDRESS")
        .unwrap_or_else(|_| "127.0.0.1:50051".to_owned())
        .parse()?;

    let solver = Solver::new();

    eprintln!(
        "bs-solver {} listening on {address}",
        bs_solver::SOLVER_VERSION
    );

    Server::builder()
        .add_service(SolverServiceServer::new(solver))
        .serve_with_shutdown(address, shutdown())
        .await?;

    Ok(())
}

/// Resolves on SIGINT, so a sweep in flight finishes rather than being cut off
/// mid-stream.
async fn shutdown() {
    let _ = tokio::signal::ctrl_c().await;
    eprintln!("bs-solver: shutting down");
}
