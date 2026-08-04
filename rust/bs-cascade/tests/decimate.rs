use bs_cascade::decimate;
use bs_frequency::Sample;

fn ramp(n: usize) -> Vec<Sample> {
    (0..n)
        .map(|i| Sample {
            #[allow(clippy::cast_precision_loss)]
            at: i as f64 * 0.01,
            hz: 60.0,
        })
        .collect()
}

/// A short trace passes through untouched — decimating 5 points into 100
/// buckets must not invent or drop any.
#[test]
fn a_trace_shorter_than_the_budget_is_unchanged() {
    let trace = ramp(5);

    assert_eq!(decimate(&trace, 100).len(), 5);
}

#[test]
fn an_empty_trace_stays_empty() {
    assert!(decimate(&[], 100).is_empty());
}

/// The point count is bounded at two per bucket.
#[test]
fn a_long_trace_is_bounded() {
    let got = decimate(&ramp(20_000), 100);

    assert!(
        got.len() <= 200,
        "got {} points, want at most 200",
        got.len()
    );
}

/// THE TEST THIS MODULE EXISTS FOR.
///
/// The nadir is printed beside the curve. A decimation that drops it puts a
/// trace on screen that disagrees with its own label, which is worse than a
/// coarse curve. A uniform stride would fail this whenever the minimum falls
/// between two strides.
#[test]
fn the_nadir_survives_decimation() {
    let mut trace = ramp(20_000);
    // A single deep, narrow excursion at an index no round stride lands on.
    trace[7_237].hz = 57.4;

    let got = decimate(&trace, 100);
    let lowest = got.iter().map(|p| p.hz).fold(f64::INFINITY, f64::min);

    assert!(
        (lowest - 57.4).abs() < 1e-12,
        "the nadir was decimated away: got {lowest}, want 57.4"
    );
}

/// An upward excursion survives too — an over-frequency island is as real as
/// an under-frequency one.
#[test]
fn a_peak_survives_decimation() {
    let mut trace = ramp(20_000);
    trace[12_911].hz = 61.5;

    let got = decimate(&trace, 100);
    let highest = got.iter().map(|p| p.hz).fold(f64::NEG_INFINITY, f64::max);

    assert!(
        (highest - 61.5).abs() < 1e-12,
        "the peak was decimated away: got {highest}"
    );
}

/// Decimation preserves time order.
#[test]
fn the_decimated_trace_is_still_monotonic() {
    let mut trace = ramp(20_000);
    trace[500].hz = 58.0;
    trace[9_000].hz = 61.0;

    let got = decimate(&trace, 100);

    for pair in got.windows(2) {
        assert!(
            pair[1].at >= pair[0].at,
            "{}s follows {}s",
            pair[1].at,
            pair[0].at
        );
    }
}
