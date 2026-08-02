//! The solver service, end to end.
//!
//! These drive the service implementation directly rather than over a socket.
//! The transport is tonic's and is not what is being tested; what is being
//! tested is the contract's semantics — that a hash mismatch is refused, that
//! an unknown model returns something a client can recover from, that SI goes
//! in and SI comes out, and that a contingency nobody can screen is not
//! reported as a safe one.

use bs_solver::proto::solver_service_server::SolverService as _;
use bs_solver::{Solver, content_hash, proto};
use prost::Message as _;
use tonic::{Code, Request};

/// A three-bus loop in SI units: 138 kV, 100 MVA base.
fn model() -> proto::NetworkModel {
    let v_base = 138_000.0;
    let s_base = 100e6;
    let z_base = v_base * v_base / s_base;

    let bus = |id: &str, kind: proto::BusKind, p_mw: f64, q_mvar: f64| proto::Bus {
        id: id.to_owned(),
        kind: kind as i32,
        p_injection_w: p_mw * 1e6,
        q_injection_var: q_mvar * 1e6,
        v_magnitude_v: v_base,
        v_angle_rad: 0.0,
        q_min_var: None,
        q_max_var: None,
        g_shunt_s: 0.0,
        b_shunt_s: 0.0,
    };

    let branch =
        |id: &str, from: u32, to: u32, r_pu: f64, x_pu: f64, rating_mva: f64| proto::Branch {
            id: id.to_owned(),
            from_bus: from,
            to_bus: to,
            r_ohm: r_pu * z_base,
            x_ohm: x_pu * z_base,
            b_shunt_s: 0.0,
            rating_va: rating_mva * 1e6,
            in_service: true,
            tap: 1.0,
            shift_rad: 0.0,
        };

    proto::NetworkModel {
        name: "three-bus".to_owned(),
        s_base_va: s_base,
        v_base_v: v_base,
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
