//! The solver service, end to end.
//!
//! These drive the service implementation directly rather than over a socket.
//! The transport is tonic's and is not what is being tested; what is being
//! tested is the contract's semantics — that a hash mismatch is refused, that
//! an unknown model returns something a client can recover from, that SI goes
//! in and SI comes out, and that a contingency nobody can screen is not
//! reported as a safe one.

use bs_solver::proto::solver_service_server::SolverService as _;
use bs_solver::{SOLVER_VERSION, Solver, content_hash, proto};
use prost::Message as _;
use tonic::{Code, Request};

/// The base every fixture in this file shares: 138 kV, 100 MVA.
const V_BASE: f64 = 138_000.0;
const S_BASE: f64 = 100e6;

fn z_base() -> f64 {
    V_BASE * V_BASE / S_BASE
}

/// A bus with no machine data — the shape every fixture used it in until the
/// cascade tests needed one with inertia and droop.
fn bus(id: &str, kind: proto::BusKind, p_mw: f64, q_mvar: f64) -> proto::Bus {
    machine_bus(id, kind, p_mw, q_mvar, None, None, None)
}

/// A bus with optional machine data — `inertia_h_s`/`droop_r` for the
/// frequency model, `p_max_mw` for dispatchable headroom. `SolvePowerFlow`
/// and `ScreenContingencies` never read these fields, so adding them here
/// changes nothing for the fixtures that only exercise those two RPCs.
fn machine_bus(
    id: &str,
    kind: proto::BusKind,
    p_mw: f64,
    q_mvar: f64,
    inertia_h_s: Option<f64>,
    droop_r: Option<f64>,
    p_max_mw: Option<f64>,
) -> proto::Bus {
    proto::Bus {
        id: id.to_owned(),
        kind: kind as i32,
        p_injection_w: p_mw * 1e6,
        q_injection_var: q_mvar * 1e6,
        v_magnitude_v: V_BASE,
        v_angle_rad: 0.0,
        q_min_var: None,
        q_max_var: None,
        g_shunt_s: 0.0,
        b_shunt_s: 0.0,
        p_min_w: None,
        p_max_w: p_max_mw.map(|p| p * 1e6),
        inertia_h_s,
        droop_r,
    }
}

fn branch(id: &str, from: u32, to: u32, r_pu: f64, x_pu: f64, rating_mva: f64) -> proto::Branch {
    proto::Branch {
        id: id.to_owned(),
        from_bus: from,
        to_bus: to,
        r_ohm: r_pu * z_base(),
        x_ohm: x_pu * z_base(),
        b_shunt_s: 0.0,
        rating_va: rating_mva * 1e6,
        in_service: true,
        tap: 1.0,
        shift_rad: 0.0,
    }
}

/// A three-bus loop in SI units: 138 kV, 100 MVA base.
fn model() -> proto::NetworkModel {
    proto::NetworkModel {
        name: "three-bus".to_owned(),
        s_base_va: S_BASE,
        v_base_v: V_BASE,
        buses: vec![
            bus("s", proto::BusKind::Slack, 0.0, 0.0),
            bus("a", proto::BusKind::Pq, -40.0, -10.0),
            bus("b", proto::BusKind::Pq, -30.0, -8.0),
        ],
        branches: vec![
            branch("s-a", 0, 1, 0.01, 0.10, 200.0),
            branch("a-b", 1, 2, 0.01, 0.15, 200.0),
            branch("b-s", 2, 0, 0.01, 0.12, 200.0),
        ],
    }
}

/// The same three-bus loop, but the slack carries machine data: inertia and
/// droop, with unlimited real-power headroom. Unlimited headroom means
/// dispatch closes any imbalance completely, so this island's frequency
/// response sees zero residual and settles `Stable` — a cascade with a
/// healthy, non-trivial frequency model behind it, rather than one that
/// settles only because there was no machine to evaluate at all.
fn cascade_model() -> proto::NetworkModel {
    let mut model = model();
    model.buses[0] = machine_bus(
        "s",
        proto::BusKind::Slack,
        0.0,
        0.0,
        Some(5.0),
        Some(0.05),
        None,
    );
    model
}

/// An island with a real but insufficient generator, connected through a
/// middle bus to a third bus that the initiating outage (branch 0) strands
/// with no generation at all.
///
/// Branch 0 (`mid-tail`) is the initiating outage in every test that uses
/// this fixture. Opening it leaves two islands:
///
/// `{gen, mid}` carries 0.5 pu of load (50 MW on this fixture's 100 MVA
/// base) against 0.42 pu of capacity — an 8 MW shortfall. With no governor
/// (`gen` states no droop) and only load damping arresting the fall, that
/// settles at 60 − 0.08 × 60 = 55.2 Hz, below a typical 57 Hz survival floor:
/// `Collapsed` with no UFLS scheme in play, exactly as
/// `an_absent_ufls_scheme_sheds_nothing` below expects. The gap is
/// deliberately this small, not the far larger one an initial draft of this
/// fixture used: an 8 MW shortfall is small enough that a single plausible
/// UFLS stage (shedding 10% of a 50 MW island, 5 MW) recovers it to 58.2 Hz —
/// back inside the band — which is what makes the absence of a scheme in
/// `an_absent_ufls_scheme_sheds_nothing` an observable claim rather than a
/// foregone one. A much larger shortfall collapses regardless of a plausible
/// scheme's shedding, and `bs_cascade::simulate` checks for a collapsed
/// island *before* it logs that step's shed events — so a fixture that
/// collapses outright would report zero `Shed` events whether or not the
/// implementation under test ever looked at the scheme at all, which is
/// exactly the kind of test that cannot fail. See the task report for how
/// this was verified.
///
/// `{tail}`, 0.3 pu of load (30 MW), has no generation at all and is dead
/// the instant it is islanded — unconditionally, independent of anything
/// above. `bs_cascade::Cascade::load_lost` only counts a fully de-energized
/// island (no surviving PV/slack member), which `{tail}` is and `{gen, mid}`
/// is not, so `load_lost_w` reflects `{tail}`'s 30 MW alone. 30 MW is still
/// tens of millions of watts, which is all `load_lost_is_returned_in_watts`
/// below needs.
fn collapsing_model() -> proto::NetworkModel {
    proto::NetworkModel {
        name: "collapsing-island".to_owned(),
        s_base_va: S_BASE,
        v_base_v: V_BASE,
        buses: vec![
            machine_bus(
                "gen",
                proto::BusKind::Slack,
                42.0,
                0.0,
                Some(3.0),
                // No droop: the point of this fixture is that nothing but
                // damping arrests the fall, so the arithmetic in the doc
                // comment above is exact rather than dependent on a governor
                // gain.
                None,
                Some(42.0),
            ),
            bus("mid", proto::BusKind::Pq, -50.0, -10.0),
            bus("tail", proto::BusKind::Pq, -30.0, 0.0),
        ],
        branches: vec![
            branch("mid-tail", 1, 2, 0.01, 0.10, 200.0),
            branch("gen-mid", 0, 1, 0.01, 0.10, 200.0),
        ],
    }
}

/// Uploads a model and returns its hash.
async fn upload(solver: &Solver, model: &proto::NetworkModel) -> String {
    let body = model.encode_to_vec();
    let hash = content_hash(&body);

    let messages = vec![
        Ok(proto::PutNetworkModelRequest {
            payload: Some(proto::put_network_model_request::Payload::Header(
                proto::PutNetworkModelHeader {
                    content_hash: hash.clone(),
                    size_bytes: body.len() as u64,
                },
            )),
        }),
        Ok(proto::PutNetworkModelRequest {
            payload: Some(proto::put_network_model_request::Payload::Chunk(body)),
        }),
    ];

    let response = solver
        .put_network_model(Request::new(stream(messages)))
        .await
        .expect("upload should succeed")
        .into_inner();

    assert_eq!(response.content_hash, hash);
    hash
}

/// A solver with [`cascade_model`] already cached, and that model's hash.
async fn cached_solver() -> (Solver, String) {
    let solver = Solver::new();
    let hash = upload(&solver, &cascade_model()).await;
    (solver, hash)
}

/// A solver with [`collapsing_model`] already cached, and that model's hash.
///
/// Also the fixture the trace tests below build on: `collapsing_model`'s
/// `{gen, mid}` island already IS a generator well short of its load, which
/// is exactly the shape a trace test needs — a machine present, and a real
/// deficit for it to respond to — so a second, differently-shaped fixture
/// would only duplicate that arithmetic.
async fn cached_solver_that_collapses() -> (Solver, String) {
    let solver = Solver::new();
    let hash = upload(&solver, &collapsing_model()).await;
    (solver, hash)
}

/// A two-stage UFLS scheme, shedding half the island's load at each stage.
/// Deliberately more aggressive than the single plausible stage
/// `collapsing_model`'s doc comment calibrates against — the trace tests
/// below only need frequency response to have run long enough to produce a
/// multi-point curve, not a specific recovered frequency, so there is no
/// reason to thread the scheme through the same tight margin the shedding
/// arithmetic elsewhere in this file depends on.
fn two_stage_scheme() -> proto::UflsScheme {
    proto::UflsScheme {
        stages: vec![
            proto::UflsStage {
                at_hz: 59.3,
                shed_fraction: 0.5,
                delay_s: 0.1,
            },
            proto::UflsStage {
                at_hz: 58.9,
                shed_fraction: 0.5,
                delay_s: 0.1,
            },
        ],
        floor_hz: 57.0,
        ceiling_hz: 61.8,
    }
}

/// Wraps messages in the Streaming type tonic hands a server method.
fn stream(
    messages: Vec<Result<proto::PutNetworkModelRequest, tonic::Status>>,
) -> tonic::Streaming<proto::PutNetworkModelRequest> {
    use tonic::codec::{Codec as _, ProstCodec};

    let body = tonic::body::boxed(frame(messages));
    let mut codec =
        ProstCodec::<proto::PutNetworkModelRequest, proto::PutNetworkModelRequest>::default();
    tonic::Streaming::new_request(codec.decoder(), body, None, None)
}

/// Applies gRPC length-prefix framing to a fixed list of messages.
///
/// One compression byte then a big-endian length, which is what tonic's
/// decoder expects. Written out because the alternative is standing up a real
/// server and a real connection to test argument validation.
fn frame(
    messages: Vec<Result<proto::PutNetworkModelRequest, tonic::Status>>,
) -> http_body_util::Full<bytes::Bytes> {
    let mut framed = bytes::BytesMut::new();

    for message in messages.into_iter().flatten() {
        let encoded = message.encode_to_vec();
        let length = u32::try_from(encoded.len()).expect("a test message under 4 GiB");

        framed.extend_from_slice(&[0]);
        framed.extend_from_slice(&length.to_be_bytes());
        framed.extend_from_slice(&encoded);
    }

    http_body_util::Full::new(framed.freeze())
}

#[tokio::test]
async fn a_model_uploads_and_is_then_known() {
    let solver = Solver::new();
    let model = model();

    let hash = upload(&solver, &model).await;

    let known = solver
        .has_network_model(Request::new(proto::HasNetworkModelRequest {
            content_hash: hash.clone(),
        }))
        .await
        .unwrap()
        .into_inner();
    assert!(known.present);

    // A second upload is reported as redundant rather than silently accepted.
    // A client re-uploading the same model has a caching bug worth surfacing.
    let body = model.encode_to_vec();
    let again = solver
        .put_network_model(Request::new(stream(vec![
            Ok(proto::PutNetworkModelRequest {
                payload: Some(proto::put_network_model_request::Payload::Header(
                    proto::PutNetworkModelHeader {
                        content_hash: hash.clone(),
                        size_bytes: body.len() as u64,
                    },
                )),
            }),
            Ok(proto::PutNetworkModelRequest {
                payload: Some(proto::put_network_model_request::Payload::Chunk(body)),
            }),
        ])))
        .await
        .unwrap()
        .into_inner();

    assert!(again.already_present);
    assert_eq!(solver.cache().len(), 1, "the same model was stored twice");
}

/// A hash that does not match the bytes must be refused. Storing it under the
/// name the client will use to find it would associate a model with a hash it
/// does not have, which defeats content addressing entirely.
#[tokio::test]
async fn a_hash_that_does_not_match_the_bytes_is_refused() {
    let solver = Solver::new();
    let body = model().encode_to_vec();

    let error = solver
        .put_network_model(Request::new(stream(vec![
            Ok(proto::PutNetworkModelRequest {
                payload: Some(proto::put_network_model_request::Payload::Header(
                    proto::PutNetworkModelHeader {
                        content_hash: "0".repeat(64),
                        size_bytes: body.len() as u64,
                    },
                )),
            }),
            Ok(proto::PutNetworkModelRequest {
                payload: Some(proto::put_network_model_request::Payload::Chunk(body)),
            }),
        ])))
        .await
        .expect_err("a mismatched hash should be refused");

    assert_eq!(error.code(), Code::InvalidArgument);
    assert!(error.message().contains("hash"), "{}", error.message());
    assert!(solver.cache().is_empty(), "the bad model was stored anyway");
}

/// The contract promises `FAILED_PRECONDITION` for an unknown model, so a
/// client knows to upload and retry once. `NOT_FOUND` would read as "give
/// up", and the
/// difference is what makes a solver restart invisible rather than an outage.
#[tokio::test]
async fn an_unknown_model_tells_the_client_how_to_recover() {
    let solver = Solver::new();

    let error = solver
        .solve_power_flow(Request::new(proto::SolvePowerFlowRequest {
            content_hash: "deadbeef".to_owned(),
            method: proto::Method::Ac as i32,
            ..Default::default()
        }))
        .await
        .expect_err("an unknown model should fail");

    assert_eq!(error.code(), Code::FailedPrecondition);
    assert!(
        error.message().contains("PutNetworkModel"),
        "the error must say how to recover: {}",
        error.message()
    );
}

/// SI in, SI out. The wire carries watts and volts; the numerics work in per
/// unit. A conversion missed in either direction produces a plausible answer
/// on the wrong scale, which is why the check is on the magnitude rather than
/// only on the shape.
#[tokio::test]
async fn results_come_back_in_si() {
    let solver = Solver::new();
    let model = model();
    let hash = upload(&solver, &model).await;

    let response = solver
        .solve_power_flow(Request::new(proto::SolvePowerFlowRequest {
            content_hash: hash.clone(),
            method: proto::Method::Ac as i32,
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();

    assert_eq!(response.buses.len(), 3);
    assert_eq!(response.branches.len(), 3);
    assert_eq!(response.model_hash, hash);
    assert!(!response.solver_version.is_empty());

    // Voltages near 138 kV, not near 1.0.
    for (index, bus) in response.buses.iter().enumerate() {
        assert!(
            bus.v_magnitude_v > 120_000.0 && bus.v_magnitude_v < 150_000.0,
            "bus {index} came back at {} volts, which is per unit escaping",
            bus.v_magnitude_v
        );
    }

    // The slack supplies the 70 MW drawn, plus losses, in watts.
    let slack = response.buses[0].p_injection_w;
    assert!(
        slack > 70e6 && slack < 80e6,
        "the slack supplied {slack} W for a 70 MW load"
    );

    // Flows are megawatts expressed in watts, not per unit.
    assert!(
        response.branches.iter().any(|b| b.p_flow_w.abs() > 1e6),
        "every branch flow is under a megawatt; these look like per unit"
    );
}

/// Proto3 gives an unset enum the zero value whether the sender meant it or
/// not. Reading that as DC would turn a forgotten field into a silently
/// different answer to the one asked for.
#[tokio::test]
async fn an_unset_method_is_refused_rather_than_defaulted() {
    let solver = Solver::new();
    let hash = upload(&solver, &model()).await;

    let error = solver
        .solve_power_flow(Request::new(proto::SolvePowerFlowRequest {
            content_hash: hash,
            method: proto::Method::Unspecified as i32,
            ..Default::default()
        }))
        .await
        .expect_err("an unset method should be refused");

    assert_eq!(error.code(), Code::InvalidArgument);
    assert!(error.message().contains("method"), "{}", error.message());
}

/// Overrides are what make the model cache worth having: a contingency is one
/// branch status against an otherwise fixed network, so the network is not
/// re-sent.
#[tokio::test]
async fn an_override_changes_the_answer_without_re_uploading() {
    let solver = Solver::new();
    let hash = upload(&solver, &model()).await;

    let base = solver
        .solve_power_flow(Request::new(proto::SolvePowerFlowRequest {
            content_hash: hash.clone(),
            method: proto::Method::Dc as i32,
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();

    let opened = solver
        .solve_power_flow(Request::new(proto::SolvePowerFlowRequest {
            content_hash: hash.clone(),
            method: proto::Method::Dc as i32,
            overrides: vec![proto::Override {
                change: Some(proto::r#override::Change::BranchStatus(
                    proto::BranchStatus {
                        branch: 1,
                        in_service: false,
                    },
                )),
            }],
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();

    assert!(
        (base.branches[1].p_flow_w - opened.branches[1].p_flow_w).abs() > 1.0,
        "opening a branch changed nothing: {} then {}",
        base.branches[1].p_flow_w,
        opened.branches[1].p_flow_w
    );
    assert!(
        opened.branches[1].p_flow_w.abs() < 1.0,
        "an out-of-service branch carries {} W",
        opened.branches[1].p_flow_w
    );

    // And the cache still holds exactly one model.
    assert_eq!(solver.cache().len(), 1);
}

/// The screen streams one result per contingency, and a contingency it could
/// not evaluate must be distinguishable from a safe one.
#[tokio::test]
async fn the_screen_streams_a_result_for_every_contingency() {
    use tokio_stream::StreamExt as _;

    let solver = Solver::new();
    let hash = upload(&solver, &model()).await;

    let mut stream = solver
        .screen_contingencies(Request::new(proto::ScreenContingenciesRequest {
            content_hash: hash,
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();

    let mut results = Vec::new();
    while let Some(item) = stream.next().await {
        results.push(item.unwrap());
    }

    assert_eq!(results.len(), 3, "one result per in-service branch");

    for result in &results {
        let contingency = result.contingency.as_ref().expect("a result named nothing");
        assert!(
            !contingency.id.is_empty(),
            "a result carried no identifier; a report would print an index at \
             someone who thinks in line names"
        );

        // Every branch of this loop is screenable, so each carries a loading.
        assert_eq!(
            result.screenability,
            proto::Screenability::Screened as i32,
            "branch {} was not screened",
            contingency.branch
        );
        assert!(result.worst_loading.is_some());
    }
}

/// A radial branch is the case where a screen most easily misleads, and the
/// wire has to preserve the distinction: absent loading, not zero loading.
#[tokio::test]
async fn a_radial_contingency_carries_no_loading_rather_than_zero() {
    use tokio_stream::StreamExt as _;

    let solver = Solver::new();

    // Add a fourth bus hanging off bus 2 on a single branch, so that branch's
    // outage islands it.
    let mut model = model();
    let tail = model.buses[2].clone();
    model.buses.push(proto::Bus {
        id: "tail".to_owned(),
        p_injection_w: -10e6,
        q_injection_var: 0.0,
        ..tail
    });
    let spur = model.branches[0].clone();
    model.branches.push(proto::Branch {
        id: "spur".to_owned(),
        from_bus: 2,
        to_bus: 3,
        ..spur
    });

    let hash = upload(&solver, &model).await;

    let mut stream = solver
        .screen_contingencies(Request::new(proto::ScreenContingenciesRequest {
            content_hash: hash,
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();

    let mut radial = None;
    while let Some(item) = stream.next().await {
        let result = item.unwrap();
        if result.screenability == proto::Screenability::Radial as i32 {
            radial = Some(result);
        }
    }

    let radial = radial.expect("the spur's outage should be reported as radial");

    assert_eq!(
        radial.worst_loading, None,
        "a contingency nobody can screen came back with a loading, so a client \
         sorting on it would rank the worst outage as the safest"
    );
    assert!(radial.violations.is_empty());
}

/// A cached model, no initiating outage, healthy network: settled, nothing
/// lost.
///
/// What would make this fail: substituting a per-unit `load_lost_w` (the
/// figure would still be `~0.0` either way, so this alone would not catch
/// that — `load_lost_is_returned_in_watts` below is the one that can);
/// forgetting to resolve `max_steps`/`horizon_s` at all and passing the raw
/// zero through to `bs_cascade::Limits`, which would make every cascade run
/// zero steps and report `Truncated`, not `Settled`; or dropping the
/// `cascade_model` machine data so the island has nothing for
/// `bs_frequency::Aggregate::of` to find, which — see `is_collapsed` in
/// `bs-cascade` — reports a loaded island with no frequency response as
/// dead, turning this into `Collapsed` rather than `Settled`.
#[tokio::test]
async fn a_cascade_on_a_healthy_model_settles() {
    let (solver, hash) = cached_solver().await;

    let response = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: hash,
            ..Default::default()
        }))
        .await
        .expect("a cached model simulates")
        .into_inner();

    assert_eq!(response.outcome, proto::CascadeOutcome::Settled as i32);
    assert!(response.events.is_empty());
    assert!((response.load_lost_w - 0.0).abs() < 1e-6);
    assert_eq!(response.solver_version, SOLVER_VERSION);
}

/// An unknown hash is `FAILED_PRECONDITION`, the same as every other method,
/// so a client's upload-and-retry-once path works here too and a solver
/// restart stays invisible.
///
/// What would make this fail: returning `NOT_FOUND`, or any code other than
/// `FAILED_PRECONDITION`; or an error message that dropped the recovery
/// instruction, leaving a client with no way to know upload-and-retry is the
/// right response.
#[tokio::test]
async fn an_unknown_model_is_failed_precondition() {
    let solver = Solver::new();

    let status = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: "0".repeat(64),
            ..Default::default()
        }))
        .await
        .expect_err("an uncached model cannot simulate");

    assert_eq!(status.code(), tonic::Code::FailedPrecondition);
    assert!(status.message().contains("PutNetworkModel"), "{status}");
}

/// Watts on the wire, per unit inside.
///
/// A load-lost figure returned in per unit would be off by the base and look
/// entirely plausible — the failure mode the SI-on-the-wire rule exists for.
///
/// What would make this fail: `to_watts` dropped from the `load_lost_w`
/// assignment in `to_cascade_response`, leaving the raw per-unit `~0.3` (the
/// dead island's load, on this fixture's 100 MVA base) in its place —
/// `0.3 < 1e6` would trip the assertion immediately.
#[tokio::test]
async fn load_lost_is_returned_in_watts() {
    let (solver, hash) = cached_solver_that_collapses().await;

    let response = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: hash,
            initiating: vec![0],
            ..Default::default()
        }))
        .await
        .expect("simulates")
        .into_inner();

    // The fixture's dead island carries 0.3 pu on a 100 MVA base, so
    // anything in the tens of millions is watts and anything near 0.3 is per
    // unit.
    assert!(
        response.load_lost_w > 1e6,
        "load lost {} looks like per unit, not watts",
        response.load_lost_w
    );
}

/// An absent UFLS scheme sheds nothing. The server supplies no default.
///
/// What would make this fail: `to_scheme`'s `None` arm substituting any
/// non-empty `Scheme` — a plausible-looking utility default, say — instead of
/// `Scheme::none()`. `collapsing_model`'s `{gen, mid}` island has real
/// machine data and a shortfall large enough (0.3 pu against 0.2 pu of
/// capacity) to cross a typical first UFLS stage (around 59.3-59.5 Hz) within
/// the first second of the trajectory, long before it goes on to breach the
/// survival floor — so a wrongly-substituted default would fire a `Shed`
/// event here, not merely leave the final numbers unchanged.
#[tokio::test]
async fn an_absent_ufls_scheme_sheds_nothing() {
    let (solver, hash) = cached_solver_that_collapses().await;

    let response = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: hash,
            initiating: vec![0],
            ufls: None,
            ..Default::default()
        }))
        .await
        .expect("simulates")
        .into_inner();

    assert!(
        !response
            .events
            .iter()
            .any(|e| e.kind == proto::CascadeEventKind::Shed as i32),
        "no scheme was supplied, so nothing may shed"
    );
}

/// A scheme with stages but an UNSET survival band must not make a healthy
/// network report `Collapsed`.
///
/// `cascade_model` settles `Stable` with no scheme at all — see
/// `a_cascade_on_a_healthy_model_settles`. This request supplies a `UflsScheme`
/// with one stage but leaves `floor_hz`/`ceiling_hz` at proto3's zero-value
/// default, exactly what a client that forgot to set the band sends on the
/// wire. `to_scheme` must read that as "the caller didn't set a band" and
/// substitute `Scheme::none()`'s 57.0/61.8, not pass `0.0` through: a settling
/// frequency of 60.0 Hz is always above a `ceiling_hz` of `0.0`, and
/// `bs_frequency::ufls::classify` reports anything above the ceiling
/// `Collapsed` — so a zero band would turn this perfectly healthy network into
/// a total collapse on the very first step.
///
/// What would make this fail: reverting `to_scheme` to copy `floor_hz`/
/// `ceiling_hz` verbatim from the wire, which would leave `ceiling_hz` at
/// `0.0` and flip `response.outcome` to `Collapsed`.
#[tokio::test]
async fn a_scheme_with_stages_and_an_unset_band_does_not_collapse_a_healthy_network() {
    let (solver, hash) = cached_solver().await;

    let response = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: hash,
            ufls: Some(proto::UflsScheme {
                stages: vec![proto::UflsStage {
                    at_hz: 59.5,
                    shed_fraction: 0.1,
                    delay_s: 0.1,
                }],
                floor_hz: 0.0,
                ceiling_hz: 0.0,
            }),
            ..Default::default()
        }))
        .await
        .expect("a cached model simulates")
        .into_inner();

    assert_eq!(
        response.outcome,
        proto::CascadeOutcome::Settled as i32,
        "a healthy network with an unset survival band must settle, not collapse"
    );
}

/// An unknown branch named in `initiating` is refused rather than handed to
/// `bs_cascade::simulate`, which panics on one. A malformed request from one
/// client must not be able to take the whole solver process down for every
/// other request in flight.
///
/// What would make this fail: removing the bounds check in
/// `simulate_cascade` and letting `bs_cascade::simulate`'s own assertion run
/// — this test would then observe a panic (a test harness abort) rather than
/// a clean `Err`, which is a much louder and more informative failure than a
/// missed assertion, but it is the reason this check exists at all.
#[tokio::test]
async fn an_out_of_range_initiating_branch_is_refused() {
    let (solver, hash) = cached_solver().await;

    let status = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: hash,
            initiating: vec![99],
            ..Default::default()
        }))
        .await
        .expect_err("an out-of-range branch should be refused");

    assert_eq!(status.code(), Code::InvalidArgument);
    assert!(status.message().contains("99"), "{status}");
}

/// A cascade where UFLS actually fires: `shed_w` comes back non-zero and in
/// WATTS, and `nadir_hz` comes back populated, on the island that shed.
///
/// Reuses `collapsing_model` with the same three-stage scheme verified in
/// `collapsing_model`'s own doc comment to recover `{gen, mid}` back inside
/// its survival band — a single plausible stage is already enough there, so
/// three gives comfortable margin for this test to observe more than one
/// `Shed` event without depending on the exact number that fire.
///
/// What would make this fail: `to_island_state` mapping `report.shed`
/// straight through without `to_watts`, leaving a per-unit magnitude — this
/// fixture sheds multiple MW per stage on a 100 MVA base, so a per-unit
/// value would be roughly `0.05`-`0.2`, nowhere near the `> 1e6` this test
/// checks for; or `bs_cascade::simulate`'s per-bus attribution never
/// reaching `IslandReport::shed`/`nadir_hz` at all, leaving them `0.0`/`None`
/// regardless of units.
#[tokio::test]
async fn ufls_shed_and_nadir_come_back_on_the_wire() {
    let (solver, hash) = cached_solver_that_collapses().await;

    let response = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: hash,
            initiating: vec![0],
            ufls: Some(proto::UflsScheme {
                stages: vec![
                    proto::UflsStage {
                        at_hz: 59.5,
                        shed_fraction: 0.1,
                        delay_s: 0.1,
                    },
                    proto::UflsStage {
                        at_hz: 59.3,
                        shed_fraction: 0.1,
                        delay_s: 0.1,
                    },
                    proto::UflsStage {
                        at_hz: 58.9,
                        shed_fraction: 0.1,
                        delay_s: 0.1,
                    },
                ],
                floor_hz: 57.0,
                ceiling_hz: 61.8,
            }),
            ..Default::default()
        }))
        .await
        .expect("simulates")
        .into_inner();

    assert!(
        response
            .events
            .iter()
            .any(|e| e.kind == proto::CascadeEventKind::Shed as i32),
        "expected at least one Shed event on this fixture"
    );

    // "gen" is bus 0 in `collapsing_model`; its island is `{gen, mid}`.
    let shedding = response
        .islands
        .iter()
        .find(|island| island.buses.contains(&0))
        .expect("the gen island is reported");

    assert!(
        shedding.shed_w > 1e6,
        "shed_w {} looks like per unit, not watts",
        shedding.shed_w
    );

    let nadir = shedding
        .nadir_hz
        .expect("a shedding island reaches a nadir");
    assert!(
        nadir > 0.0 && nadir < 60.0,
        "nadir {nadir} Hz is not a plausible dip below nominal"
    );
}

/// An island with no machine data anywhere reports its three frequency
/// fields ABSENT, never a fabricated zero.
///
/// Reuses `collapsing_model`'s "tail" island — isolated with no machine data
/// from the very first instant of the run, per `collapsing_model`'s own
/// fixture, never transiently sharing an island with "gen".
///
/// What would make this fail: mapping `None` to `Some(0.0)` anywhere along
/// the chain — in `bs_cascade::simulate`'s per-bus attribution or in
/// `to_island_state`'s field-by-field copy — or a per-bus attribution bug
/// that lets "tail" pick up a stale value left over from a different
/// island's history.
#[tokio::test]
async fn a_machineless_islands_frequency_fields_are_absent_on_the_wire() {
    let (solver, hash) = cached_solver_that_collapses().await;

    let response = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: hash,
            initiating: vec![0],
            ..Default::default()
        }))
        .await
        .expect("simulates")
        .into_inner();

    let dead = response
        .islands
        .iter()
        .find(|island| island.buses == vec![2])
        .expect("the tail island, bus 2 alone, is reported");

    assert_eq!(dead.frequency_hz, None);
    assert_eq!(dead.nadir_hz, None);
    assert_eq!(dead.rocof_hz_s, None);
}

/// The curve crosses in HERTZ. A frequency divided by a power base is the
/// exact error the SI-on-the-wire rule exists to prevent, and it would look
/// entirely plausible: a 60 Hz trace on a 100 MVA base becomes 6e-7.
///
/// What would make this fail: `to_island_state` mapping `s.hz` through
/// `to_watts` or any other per-unit conversion instead of copying it
/// straight through — every point would land near 6e-7, far outside this
/// test's 50-70 Hz band, not merely off by a plausible-looking amount.
#[tokio::test]
async fn the_frequency_trace_crosses_in_hertz() {
    let (solver, hash) = cached_solver_that_collapses().await;

    let response = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: hash,
            initiating: vec![0],
            ufls: Some(two_stage_scheme()),
            ..Default::default()
        }))
        .await
        .expect("simulates")
        .into_inner();

    let traced = response
        .islands
        .iter()
        .find(|i| !i.trace.is_empty())
        .expect("an island with a machine has a curve");

    for point in &traced.trace {
        assert!(
            point.hz > 50.0 && point.hz < 70.0,
            "{} Hz is not a frequency — it has been divided by something",
            point.hz
        );
    }
}

/// The curve and the event log share one clock.
///
/// What would make this fail: dropping the step offset when the trace is
/// accumulated across steps in `bs_cascade::simulate` (the defect
/// `bs-cascade/tests/trace.rs::the_trace_is_on_the_run_clock_and_never_goes_backwards`
/// guards on the domain side), or `to_island_state` reordering points while
/// mapping them onto the wire type.
#[tokio::test]
async fn the_trace_is_on_the_same_clock_as_the_events() {
    let (solver, hash) = cached_solver_that_collapses().await;

    let response = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: hash,
            initiating: vec![0],
            ufls: Some(two_stage_scheme()),
            ..Default::default()
        }))
        .await
        .expect("simulates")
        .into_inner();

    let traced = response
        .islands
        .iter()
        .find(|i| !i.trace.is_empty())
        .expect("a curve");

    for pair in traced.trace.windows(2) {
        assert!(
            pair[1].at_s >= pair[0].at_s,
            "the curve went backwards on the wire"
        );
    }
}

/// The initiating outage comes back as a branch id, not an index.
///
/// What would make this fail: assigning `request.initiating` (the raw
/// `Vec<u32>`, rendered to strings) to the response field instead of
/// resolving each index against `case.branches` — the parse-as-u32 check
/// below exists specifically to catch that substitution, since a
/// stringified index would otherwise look like a plausible, non-empty name.
#[tokio::test]
async fn the_initiating_outage_is_named() {
    let (solver, hash) = cached_solver().await;

    let response = solver
        .simulate_cascade(Request::new(proto::SimulateCascadeRequest {
            content_hash: hash,
            initiating: vec![0],
            ..Default::default()
        }))
        .await
        .expect("simulates")
        .into_inner();

    assert_eq!(response.initiating.len(), 1);
    assert!(
        !response.initiating[0].is_empty(),
        "an empty name is no better than an index"
    );
    assert!(
        response.initiating[0].parse::<u32>().is_err(),
        "got {:?}, which is an index rendered as a string, not a branch id",
        response.initiating[0]
    );
}
