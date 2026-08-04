//! Bounding a frequency curve for display without lying about it.
//!
//! An integration at 10 ms over a 60 s horizon produces thousands of points
//! per step, and a scrubber needs a couple of hundred. The question is which
//! ones to keep.
//!
//! Not a uniform stride. A stride drops whatever falls between its samples,
//! and the one point this curve must never lose is the nadir: it is displayed
//! as a number beside the curve, so a stride that skipped it would put a trace
//! on screen that disagrees with its own label.
//!
//! Min/max decimation instead — the standard waveform-display reduction. Each
//! equal-time bucket contributes its lowest and highest point, in the order
//! they occurred. Every local excursion survives at the resolution shown, and
//! the global nadir survives by construction as the minimum of whichever
//! bucket holds it.

use bs_frequency::Sample;

/// Buckets used for a run's trace. At two points per bucket this bounds a
/// curve at 200 points, which is more than a scrubber resolves.
pub const TRACE_BUCKETS: usize = 100;

/// Reduces a trace to at most `2 * buckets` points, preserving extrema.
///
/// A trace already shorter than the budget is returned unchanged rather than
/// resampled: resampling short input would move points that were exact.
#[must_use]
pub fn decimate(trace: &[Sample], buckets: usize) -> Vec<Sample> {
    if buckets == 0 || trace.len() <= buckets * 2 {
        return trace.to_vec();
    }

    let mut out = Vec::with_capacity(buckets * 2);

    // Bucketing is by INDEX rather than by time. The samples are produced at a
    // fixed integration step, so index buckets are time buckets — and index
    // arithmetic avoids a float-to-integer cast, which the workspace denies.
    for bucket in 0..buckets {
        let start = trace.len() * bucket / buckets;
        let end = trace.len() * (bucket + 1) / buckets;

        let slice = &trace[start..end];
        if slice.is_empty() {
            continue;
        }

        let mut lowest = 0;
        let mut highest = 0;
        for (index, point) in slice.iter().enumerate() {
            if point.hz < slice[lowest].hz {
                lowest = index;
            }
            if point.hz > slice[highest].hz {
                highest = index;
            }
        }

        // In the order they occurred, so the reduced curve stays monotonic in
        // time. Equal indices mean a flat bucket: emit one point, not two.
        match lowest.cmp(&highest) {
            std::cmp::Ordering::Equal => {
                out.push(slice[lowest]);
            }
            std::cmp::Ordering::Less => {
                out.push(slice[lowest]);
                out.push(slice[highest]);
            }
            std::cmp::Ordering::Greater => {
                out.push(slice[highest]);
                out.push(slice[lowest]);
            }
        }
    }

    out
}
