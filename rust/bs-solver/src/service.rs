//! The gRPC service.
//!
//! `tonic::Status` is a large error type and every method here returns one, so
//! `result_large_err` is allowed for the module rather than boxed at each site.
//! Boxing would add an allocation on a path that is already crossing a network
//! and would obscure the type the contract actually specifies.
#![allow(clippy::result_large_err)]

use std::sync::Arc;

use bs_contingency::{Contingency, ScreenOutcome, all_branches, screen};
use bs_factors::Lodf;
use bs_powerflow::ac::{Options, solve_ac_from, solve_with_limits};
use bs_powerflow::{Voltages, solve_dc};
use prost::Message as _;
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::{Request, Response, Status, Streaming};

use crate::cache::{Entry, ModelCache, content_hash};
use crate::convert::{apply_overrides, to_case, to_volts, to_watts};
use crate::proto;

/// The solver's own version, reported with every result so a number can be
/// traced to the code that produced it.
pub const SOLVER_VERSION: &str = env!("CARGO_PKG_VERSION");

/// The solver service.
#[derive(Debug, Default)]
pub struct Solver {
    cache: Arc<ModelCache>,
}

impl Solver {
    /// A solver with an empty cache.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// The model cache, exposed for tests and for a health check that reports
    /// how many models are held.
    #[must_use]
    pub fn cache(&self) -> &Arc<ModelCache> {
        &self.cache
    }

    /// Looks up a model, or returns the typed error the contract promises.
    ///
    /// `FAILED_PRECONDITION` rather than `NOT_FOUND`, because the client's correct
    /// response is to upload and retry exactly once — a precondition it can
    /// satisfy — rather than to give up. That distinction is what makes a
    /// solver restart invisible instead of an outage.
    fn lookup(&self, hash: &str) -> Result<Entry, Status> {
        self.cache.get(hash).ok_or_else(|| {
            Status::failed_precondition(format!(
                "model {hash} is not cached; upload it with PutNetworkModel and retry"
            ))
        })
    }
}

/// Narrows an index for the wire.
///
/// The contract types every index as `uint32`, so a model with more than four
/// billion elements cannot be expressed on this wire at all and would have
/// been rejected at upload. The panic is therefore unreachable through the
/// service, and is kept rather than saturated because a saturated index is a
/// wrong index — it would name element 4294967295 in a report, which is worse
/// than stopping.
fn index(value: usize) -> u32 {
    u32::try_from(value).expect("an index larger than the wire format can express")
}

fn options(tolerance_w: f64, max_iterations: u32, s_base_va: f64) -> Options {
    let mut opts = Options::default();

    // The wire carries a tolerance in watts because every wire value is SI;
    // the solver judges convergence in per unit. Zero means "use the default"
    // rather than "converge exactly", which no floating-point solve can do.
    if tolerance_w > 0.0 && s_base_va > 0.0 {
        opts.tolerance = tolerance_w / s_base_va;
    }

    if max_iterations > 0 {
        opts.max_iterations = max_iterations as usize;
    }

    opts
}

#[tonic::async_trait]
impl proto::solver_service_server::SolverService for Solver {
    async fn put_network_model(
        &self,
        request: Request<Streaming<proto::PutNetworkModelRequest>>,
    ) -> Result<Response<proto::PutNetworkModelResponse>, Status> {
        let mut stream = request.into_inner();

        let mut header: Option<proto::PutNetworkModelHeader> = None;
        let mut body = Vec::new();

        while let Some(message) = stream.message().await? {
            match message.payload {
                Some(proto::put_network_model_request::Payload::Header(sent)) => {
                    if header.is_some() {
                        return Err(Status::invalid_argument("a second header was sent"));
                    }

                    // Reserving up front is the point of sending a size, but a
                    // hostile or buggy client must not be able to name a size
                    // that exhausts memory before a single byte arrives.
                    body.reserve(usize::try_from(sent.size_bytes).unwrap_or(0).min(1 << 28));
                    header = Some(sent);
                }
                Some(proto::put_network_model_request::Payload::Chunk(chunk)) => {
                    if header.is_none() {
                        return Err(Status::invalid_argument(
                            "a chunk arrived before the header; the hash cannot be \
                             verified without knowing what it should be",
                        ));
                    }
                    body.extend_from_slice(&chunk);
                }
                None => return Err(Status::invalid_argument("an empty message was sent")),
            }
        }

        let header = header.ok_or_else(|| Status::invalid_argument("no header was sent"))?;

        if body.len() as u64 != header.size_bytes {
            return Err(Status::invalid_argument(format!(
                "the header promised {} bytes and {} arrived",
                header.size_bytes,
                body.len()
            )));
        }

        let computed = content_hash(&body);
        if computed != header.content_hash {
            // The client's own hash is wrong, or the bytes changed in transit.
            // Either way storing this under the name the client will use to
            // find it would silently associate a model with the wrong hash.
            return Err(Status::invalid_argument(format!(
                "the header claimed {} and the bytes hash to {computed}",
                header.content_hash
            )));
        }

        if self.cache.contains(&computed) {
            return Ok(Response::new(proto::PutNetworkModelResponse {
                content_hash: computed,
                already_present: true,
            }));
        }

        let model = proto::NetworkModel::decode(body.as_slice())
            .map_err(|e| Status::invalid_argument(format!("the model did not decode: {e}")))?;

        // Converted and validated here rather than at first solve, so a
        // malformed model is rejected by the call that supplied it instead of
        // by an unrelated request later.
        let case = to_case(&model).map_err(|e| Status::invalid_argument(e.to_string()))?;
        case.validate()
            .map_err(|e| Status::invalid_argument(format!("the model is not solvable: {e}")))?;

        let already_present = self.cache.insert(computed.clone(), Entry { model, case });

        Ok(Response::new(proto::PutNetworkModelResponse {
            content_hash: computed,
            already_present,
        }))
    }

    async fn has_network_model(
        &self,
        request: Request<proto::HasNetworkModelRequest>,
    ) -> Result<Response<proto::HasNetworkModelResponse>, Status> {
        Ok(Response::new(proto::HasNetworkModelResponse {
            present: self.cache.contains(&request.into_inner().content_hash),
        }))
    }

    async fn solve_power_flow(
        &self,
        request: Request<proto::SolvePowerFlowRequest>,
    ) -> Result<Response<proto::SolvePowerFlowResponse>, Status> {
        let request = request.into_inner();
        let entry = self.lookup(&request.content_hash)?;

        let mut case = entry.case.clone();
        apply_overrides(&mut case, &request.overrides);

        let opts = options(
            request.tolerance_w,
            request.max_iterations,
            entry.model.s_base_va,
        );

        let response = match proto::Method::try_from(request.method) {
            Ok(proto::Method::Dc) => {
                let solution = solve_dc(&case)
                    .map_err(|e| Status::invalid_argument(format!("DC solve: {e}")))?;

                // DC has no voltages: every magnitude is 1.0 per unit by
                // construction and there is no reactive power at all. Reported
                // as such rather than left at zero, which would read as a
                // collapsed network.
                let buses = (0..case.n_bus())
                    .map(|bus| proto::BusResult {
                        v_magnitude_v: entry.model.v_base_v,
                        v_angle_rad: solution.angles[bus],
                        p_injection_w: to_watts(case.buses[bus].p_injection, &entry.model),
                        q_injection_var: 0.0,
                    })
                    .collect();

                proto::SolvePowerFlowResponse {
                    buses,
                    branches: branch_results(&case, &solution.flows, &entry.model),
                    iterations: 1,
                    residual_w: 0.0,
                    binding_generators: Vec::new(),
                    solver_version: SOLVER_VERSION.to_owned(),
                    model_hash: request.content_hash,
                }
            }

            Ok(proto::Method::Ac) => {
                let (voltages, injections, flows, iterations, residual, binding) =
                    if request.enforce_reactive_limits {
                        let limited = solve_with_limits(&case, opts)
                            .map_err(|e| Status::invalid_argument(format!("AC solve: {e}")))?;
                        let binding = limited.binding().iter().copied().map(index).collect();
                        let solution = limited.solution;
                        (
                            solution.voltages,
                            solution.injections,
                            solution.flows,
                            solution.iterations,
                            solution.residual,
                            binding,
                        )
                    } else {
                        let solution = solve_ac_from(&case, &Voltages::flat(&case), opts)
                            .map_err(|e| Status::invalid_argument(format!("AC solve: {e}")))?;
                        (
                            solution.voltages,
                            solution.injections,
                            solution.flows,
                            solution.iterations,
                            solution.residual,
                            Vec::new(),
                        )
                    };

                let buses = (0..case.n_bus())
                    .map(|bus| proto::BusResult {
                        v_magnitude_v: to_volts(voltages.magnitude[bus], &entry.model),
                        v_angle_rad: voltages.angle[bus],
                        p_injection_w: to_watts(injections.p[bus], &entry.model),
                        q_injection_var: to_watts(injections.q[bus], &entry.model),
                    })
                    .collect();

                proto::SolvePowerFlowResponse {
                    buses,
                    branches: branch_results(&case, &flows, &entry.model),
                    iterations: index(iterations),
                    residual_w: to_watts(residual, &entry.model),
                    binding_generators: binding,
                    solver_version: SOLVER_VERSION.to_owned(),
                    model_hash: request.content_hash,
                }
            }

            _ => {
                return Err(Status::invalid_argument(
                    "no method was specified; proto3 defaults an unset enum to zero, \
                     so this is a missing field rather than a choice of DC",
                ));
            }
        };

        Ok(Response::new(response))
    }

    type ScreenContingenciesStream =
        ReceiverStream<Result<proto::ScreenContingenciesResponse, Status>>;

    async fn screen_contingencies(
        &self,
        request: Request<proto::ScreenContingenciesRequest>,
    ) -> Result<Response<Self::ScreenContingenciesStream>, Status> {
        let request = request.into_inner();
        let entry = self.lookup(&request.content_hash)?;

        let mut case = entry.case.clone();
        apply_overrides(&mut case, &request.overrides);

        let pre = solve_dc(&case)
            .map_err(|e| Status::invalid_argument(format!("base case DC solve: {e}")))?;
        let lodf = Lodf::build(&case)
            .map_err(|e| Status::invalid_argument(format!("distribution factors: {e}")))?;

        let contingencies: Vec<Contingency> = if request.contingencies.is_empty() {
            all_branches(&case)
        } else {
            request
                .contingencies
                .iter()
                .map(|wire| Contingency::new(wire.id.clone(), wire.branch as usize))
                .collect()
        };

        // Resolved before screening, because the screen collects at this
        // threshold rather than collecting at the rating and filtering after.
        // Filtering a filtered list is what let a request for anything below
        // 1.0 be silently ignored.
        let threshold = if request.violation_threshold > 0.0 {
            request.violation_threshold
        } else {
            1.0
        };

        let results = screen(&case, &lodf, &pre.flows, &contingencies, threshold)
            .map_err(|e| Status::internal(format!("screen: {e}")))?;

        // A bounded channel so a slow client applies backpressure rather than
        // letting the server buffer an entire sweep in memory. The screen is
        // already computed, so this only paces delivery.
        let (tx, rx) = mpsc::channel(64);
        let model = entry.model.clone();
        drop(case);

        tokio::spawn(async move {
            for result in results {
                let wire = to_wire(&result, &model);
                if tx.send(Ok(wire)).await.is_err() {
                    // The client hung up. Stopping is correct — nobody is
                    // waiting for the rest.
                    break;
                }
            }
        });

        Ok(Response::new(ReceiverStream::new(rx)))
    }
}

fn branch_results(
    case: &bs_case::Case,
    flows: &[f64],
    model: &proto::NetworkModel,
) -> Vec<proto::BranchResult> {
    case.branches
        .iter()
        .enumerate()
        .map(|(index, branch)| proto::BranchResult {
            p_flow_w: to_watts(flows[index], model),
            // Unrated means the limit is unknown, so the loading is reported
            // as zero rather than as infinity.
            loading: if branch.rating > 0.0 {
                flows[index].abs() / branch.rating
            } else {
                0.0
            },
        })
        .collect()
}

fn to_wire(
    result: &bs_contingency::ScreenResult,
    model: &proto::NetworkModel,
) -> proto::ScreenContingenciesResponse {
    let contingency = Some(proto::Contingency {
        id: result.contingency.id.clone(),
        branch: index(result.contingency.branch),
    });

    match &result.outcome {
        ScreenOutcome::Screened {
            violations,
            worst_loading,
        } => proto::ScreenContingenciesResponse {
            contingency,
            screenability: proto::Screenability::Screened as i32,
            // Not filtered again. The screen already collected at the
            // caller's threshold, and a second filter here is what hid the
            // fact that the first one ignored it.
            violations: violations
                .iter()
                .map(|v| proto::Violation {
                    branch: index(v.branch),
                    p_flow_w: to_watts(v.flow, model),
                    rating_va: to_watts(v.rating, model),
                    loading: v.loading,
                })
                .collect(),
            worst_loading: Some(*worst_loading),
        },

        // worst_loading stays absent, not zero. A contingency that could not
        // be evaluated is not a safe one, and a client sorting on a zero would
        // rank it below every mildly loaded case — burying exactly the outage
        // that most needs simulating.
        ScreenOutcome::Radial => proto::ScreenContingenciesResponse {
            contingency,
            screenability: proto::Screenability::Radial as i32,
            violations: Vec::new(),
            worst_loading: None,
        },

        ScreenOutcome::NotApplicable => proto::ScreenContingenciesResponse {
            contingency,
            screenability: proto::Screenability::NotApplicable as i32,
            violations: Vec::new(),
            worst_loading: None,
        },
    }
}
