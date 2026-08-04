//! The solver server.

use std::net::SocketAddr;

use bs_solver::proto::solver_service_server::SolverServiceServer;
use bs_solver::weather_proto::weather_grid_service_server::WeatherGridServiceServer;
use bs_solver::{Solver, WeatherGrid};
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

    // Two services, one process. gRPC multiplexes them by path, so a client
    // reaches either over the same connection — which is the whole reason the
    // weather decoder does not need a deployment of its own.
    let weather = WeatherGrid::new();

    eprintln!(
        "bs-solver {} listening on {address} (solver, weather-grid)",
        bs_solver::SOLVER_VERSION
    );

    Server::builder()
        .add_service(SolverServiceServer::new(solver))
        .add_service(WeatherGridServiceServer::new(weather))
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
