# Roadmap

What is built, what is being built, what comes next, and what will not be
built at all. Ordering is real; dates are not given, because this is one
person's project and a date would be a guess presented as a commitment.

Current state is summarised in the [readme](README.md#status). This
document is the longer version and the reasoning behind the order.

---

## Delivered

| Milestone | What it means |
|---|---|
| **Foundation** | Host, extension composition and dependency injection, migrations, provenance, audit, continuous integration. Bitemporality landed here rather than later, because it cannot be retrofitted: adding it afterwards invalidates every evaluation run before it. |
| **Network model** | Topology, geospatial routing, importers for MATPOWER and PSS/E, and a viewer. |
| **Numerical core** | Sparse AC and DC power flow, distribution factors, N-1 screening. Rust. Agrees with committed pandapower reference solutions on IEEE 14, 30, 57, 118, 300 and PEGASE 2869. |
| **IEC 61850 SCL** | Substation configuration parsing. Reads configuration files and originates nothing. |
| **Weather and hazard** | HRRR retrieval by byte range against the GRIB2 index sidecars, a first-party decoder checked against ecCodes, per-span sampling, and per-span wind loading, ice accretion and IEEE 738 dynamic ratings. |
| **Fragility** | Per-component failure probability from published lognormal curves, each median and dispersion carrying a citation. Landed 8 August 2026. |

## In progress

**Cascade.** Propagation with protection, islanding and under-frequency
load shedding; scenario orchestration; blast-radius geometry. The engine
runs. What it lacks is validation against anything external: no
historical cascade has been reproduced, and no simulated outage footprint
has been compared against an observed one.

---

## Next, and why in this order

### 1. Outage-history ingest

The ORNL EAGLE-I county-level record and the DOE OE-417 disturbance
reports. Nothing downstream can be calibrated or scored without an
observed record to calibrate and score against, so this blocks everything
below it.

### 2. Calibration

The fragility curves currently carry their published values and have been
fitted to nothing. Fitting them against the observed record, under
knowledge-time discipline so that a fit cannot read a correction issued
after the event, is what turns a plausible curve into a defensible one.

Note the resolution mismatch honestly: EAGLE-I is county-level and the
quantity being fitted is per-component. Whether that transfer works is an
open question, not a detail.

### 3. Backtest

The bitemporal machinery exists precisely for this and has never been
used for it. Until a backtest runs, the system makes no accuracy claim of
any kind, and none should be inferred.

### 4. Risk-ordered contingency selection

The step that closes the pipeline. Per-component failure probabilities
become the sampling distribution for cascade initiation, replacing the
fixed credible-contingency list that contingency analysis conventionally
enumerates. Time-resolved, so a front crossing a territory produces
correlated non-simultaneous initiating events rather than one
simultaneous set.

The seam already exists and is deliberate: the contingency engine takes
its contingency list and ordering from the caller. Nothing yet supplies
that ordering from a probability model.

This is the piece that makes the system do what it was designed to do. It
is fourth rather than first because doing it before calibration would
produce a well-engineered number with nothing behind it.

### 5. Grid protocol clients

DNP3, IEC 60870-5-104, and the MMS and GOOSE halves of IEC 61850. None
exists today. All are read-only when they arrive, and the lint that
enforces that already covers the packages they will live in.

### 6. Visual system

The operator console renders a hazard layer today. The full geospatial
and temporal surface is a later concern than the numbers it would
display.

---

## Research track

Directions the architecture is meant to support. None is implemented,
none is scheduled, and each is recorded here so the scope of the design is
legible rather than as claimed capability.

- **Rare-event estimation.** Multilevel splitting for tail probabilities,
  reported with confidence intervals instead of truncating the
  distribution where naive Monte Carlo runs out of samples. Established
  in the literature and almost never present in deployable software, so
  this is engineering rather than research.
- **Hazard-conditioned influence graph.** Edge weights as a function of
  the current hazard field rather than fixed. The literature's stated
  obstacle is data scarcity, which a fast simulator generating traces
  under sampled weather addresses directly.
- **Conformal coverage.** Distribution-free guarantees on risk
  statements, with empirical coverage reported in the backtest.
- **Weather ensembles.** Propagating many members through hazard,
  fragility and cascade. The known caveat is that AI weather models
  underestimate extreme percentiles, and extremes are what damage grids.
- **Differentiable cascade.** Fitting unobservable parameters, protection
  settings especially, from observed outage footprints. High risk;
  discrete cascades may admit no useful gradients.

---

## Not planned

**Actuation, in any form.** No control command to any device, ever. This
is an architectural boundary enforced by a check in continuous
integration, not a limitation of the current stage, and it will not be
relaxed. A system that ranks contingencies by probability is one command
away from acting on them, which is exactly why the line is drawn at the
repository boundary rather than at a configuration flag. See
[docs/SAFETY.md](docs/SAFETY.md).

**Published results for real, named utilities.** Analysis is published
only for synthetic test systems. The Texas A&M ACTIVSg cases exist so
this work can be done in the open without touching Critical
Energy/Electric Infrastructure Information.

**Vegetation modelling**, for now. It appears in no current milestone
despite being a leading cause of distribution outages, because the public
data to do it credibly at span resolution is thin.

---

## What would change this order

A calibration result showing that county-level outage counts cannot
usefully constrain per-component fragility would make items 2 through 4
much harder and would be worth knowing early. If that turns out to be the
case, it will be written up rather than quietly dropped.
