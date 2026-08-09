# Blackstart

**A digital twin of a United States electric power network, built to answer what severe
weather does to it.**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8.svg)](go.mod)
[![Rust](https://img.shields.io/badge/rust-see%20toolchain-DEA584.svg)](rust/rust-toolchain.toml)
[![Status](https://img.shields.io/badge/status-early%20development-orange.svg)](#status)

> **Early development.** Phases 0 through 3 of nine are complete, phase 4 is in progress,
> and the first piece of the prediction layer landed on 8 August 2026. The network model,
> the Rust solver, the weather-to-line-rating path and the fragility layer run end to end.
> There is no backtest and no calibrated model, so the system makes no accuracy claim of
> any kind. Nothing here is ready to be relied upon and there are no releases.

Blackstart ingests public numerical weather prediction, derives the engineering
quantities that break equipment, and attaches them to a versioned, geographically routed
model of a transmission network. Weather is sampled along each conductor span rather than
at one point per line, because a 30 km line does not experience one gust.

Every figure it produces traces to a dataset version, a model version, and a code commit.
Facts carry two time axes: when they were true of the world, and when the system learned
them. A model evaluated as of a past moment resolves its inputs at that moment, never
against a record corrected a week later.

---

## Contents

[What is built](#what-is-built) · [Positioning](#positioning) · [Validation](#validation) ·
[Advisory only](#advisory-only) · [Public data only](#public-data-only) ·
[Open-core boundary](#open-core-boundary) · [Architecture](#architecture) ·
[Status](#status) · [Building](#building)

---

## What is built

Measured against commit `c3c93c2b` on 9 August 2026.

| | |
|---|---|
| Extensions | 15 |
| Go | 496 files, 99,506 lines |
| Rust | 12 crates, 20,575 lines |
| Tests | 1,326 Go functions, 312 Rust functions |
| Commits | 703 |

**Runs end to end.** Network import and geospatial routing. AC and DC power flow and N-1
screening in Rust. IEC 61850 SCL parsing. NOAA HRRR retrieval by byte range against the
GRIB2 index sidecars, decoded by a first-party decoder and sampled onto individual spans.
Per-span wind loading, ice accretion and IEEE 738 dynamic ratings. Per-component failure
probability from published fragility curves. Cascade propagation with protection,
islanding and under-frequency load shedding.

**Does not exist.** Outage-history ingest. Any calibration of the fragility curves against
an observed record. A backtest harness. Contingency selection driven by failure
probability, which is the step that would close the pipeline. Protocol clients of any
kind. Vegetation modelling.

## Positioning

Coupling weather-derived component fragility to a network model to estimate consequence
is established practice, not a gap. Panteli, Pickering, Wilkinson, Dawson and Mancarella
published it in 2017, over optimal power flow and sequential Monte Carlo, for a windstorm
moving across a transmission network. The AC cascading failure model of Noebels, Preece
and Panteli was later extended from simultaneous contingencies to a discretised weather
event at hourly intervals. Karagiannakis and colleagues reviewed the field in 2025.

Nor are the components new here. Outage-count prediction from weather is published since
2014 and operationally deployed. Rare-event splitting for blackout probability is Shortle
and colleagues and Bae and Thorp. Influence-graph cascade models are Dobson, Hines and
colleagues. Learned power-flow surrogates have been active since 2023. Grid digital twins
are a commercial category, and versioned network models with audit trails are ordinary
utility practice, specified by EPRI and operated at ERCOT and CAISO.

**What this project is, then.** That literature produces studies: a methodology run to
yield a result. This is an attempt to build the thing underneath one, maintained rather
than assembled per analysis, and to give it away. Three properties follow from that:

- **Knowledge time is enforced in storage.** Every fact records when it was learned, and
  an evaluation as of a past instant cannot read a record corrected afterwards. Model
  management answers what the network was configured to be; this answers what was
  believed at the time, which is the question a backtest actually asks.
- **It runs on public data only**, so a result is reproducible by someone who does not
  work at a utility.
- **It is open.** Apache-2.0, adoptable without a transaction, which matters most to the
  cooperative and municipal utilities that cannot commission this analysis commercially.

## Validation

The claims worth weighing are the ones checked against something outside this project.

| Component | Checked against | Result |
|---|---|---|
| AC and DC power flow | Committed pandapower reference solutions: IEEE 14, 30, 57, 118, 300 and PEGASE 2869 | Agrees within committed per-quantity tolerances, each with a written rationale |
| GRIB2 decoder | ecCodes, run out of process, on three real NOAA messages | Agrees on strided samples and whole-field statistics |
| Solver internals | Power balance, nodal balance and linearity, independent of any oracle | Holds; exists to catch a solver and its golden being wrong the same way |
| Determinism | Same case solved twice in one process | Byte-identical event sequence, load loss and outcome |
| Cascade outcomes | Nothing external | **Not validated.** No historical cascade reproduced |

Tolerances are committed with reasoning attached. A failing comparison is a solver defect,
never an occasion to widen a tolerance.

### A measured result

Dynamic line ratings computed twice for the same span and valid time, once from the
forecast an operator held and once from the analysis that exists only afterwards, across
39,501 span-hours on 216 rated spans:

| Lead | Mean | Median | Max | Differ by >5% | Differ by >100 A |
|---|---|---|---|---|---|
| 1 hour | 62.3 A | 49.7 A | 500.6 A | 45.4% | 19.5% |
| 6 hours | 87.7 A | 71.4 A | 505.2 A | 55.7% | 38.1% |
| 12 hours | 120.7 A | 102.2 A | 572.1 A | 69.2% | 50.8% |

Mean rating over the period is 1,181 A. Both runs read the same assumed conductor, so the
conductor assumption cancels and what remains is the weather vintage. This is divergence
against a later model analysis rather than error against truth, it covers one network and
one week of August with no icing, and it measures an input to a risk model rather than the
skill of one. Method and query: [`docs/extensions/hazard.md`](docs/extensions/hazard.md).

## Advisory only

**Blackstart produces forecasts, rankings and recommendations. It does not act on them.**

No code in this repository writes to a SCADA, EMS, ADMS, RTU, relay or any other field
device, and no endpoint accepts an instruction destined for one. There is no protocol
client here at all: no MMS, GOOSE, DNP3 or IEC 60870-5-104. Phase 5 has not started. What
exists is the IEC 61850 SCL parser, which reads substation configuration files and
originates nothing.

The read-only rule therefore binds adapters when they are written. A lint in the required
CI check already covers the packages they will live in, and rejects identifiers matching
write, send, issue, execute, operate or set combined with control, command, setpoint,
breaker, point, output or tap. A system that ranks contingencies by probability is one
command away from acting on them, which is why the line sits at the repository boundary
rather than at a configuration flag. See [docs/SAFETY.md](docs/SAFETY.md).

## Public data only

Every dataset is downloadable without an NDA, a licence negotiation or a utility
relationship. Every ingest records the source URL, retrieval timestamp, content hash and
parser version.

| Source | Provides |
|---|---|
| [Texas A&M ACTIVSg synthetic grids](https://electricgrids.engr.tamu.edu) | Realistic synthetic networks free of CEII restriction |
| IEEE standard test cases (14/30/57/118/300) | Solver validation |
| NOAA HRRR, NBM, NDFD, GFS, GEFS, HREF | Numerical weather prediction (GRIB2) |
| NOAA MRMS | Radar-derived precipitation and hail |
| NWS ASOS/METAR | Surface observations |
| [ORNL EAGLE-I](https://eagle-i.doe.gov) | County-level historical outage counts |
| [DOE OE-417](https://doe417.pnnl.gov) | Major electric disturbance reports |
| EIA-860 / 861 / 930 | Generator inventory, reliability metrics, hourly BA operations |
| NERC TADS / DADS | Transmission and distribution outage statistics, aggregate only |
| [EIA U.S. Energy Atlas](https://atlas.eia.gov) | Transmission line geospatial layer, 69–765 kV |
| [PNNL-33587](https://www.pnnl.gov/main/publications/external/technical_reports/PNNL-33587.pdf) | Fragility functions resource report, and the literature it catalogues |
| [OpenStreetMap power layer](https://overpass-api.de) | Per-structure geometry (ODbL 1.0, share-alike) |
| USGS 3DEP | Terrain elevation |
| NLCD / LANDFIRE | Land cover |
| ENTSO-E CGMES conformity models | CIM/CGMES parser test data |
| Public IEC 61850 sample SCD/ICD files | SCL parser test data |

### Security and CEII

**Analysis results are published only for synthetic test systems, never for real named
utilities.** The ACTIVSg cases exist so this work can be done in the open without touching
Critical Energy/Electric Infrastructure Information. They are statistically and
geographically realistic and correspond to no real network. Contributions containing
results for a real, named utility will not be accepted.

For software vulnerabilities, see [SECURITY.md](SECURITY.md).

## Open-core boundary

**Open, Apache-2.0, here:** the simulation engine; all parsers and importers; all data
connectors; the grid domain model including the bitemporal and provenance machinery; the
benchmark suite and numerical validation record; the reference web client.

**Open, separately licensed:** OSM-derived route and structure geometry ships as a
standalone **ODbL 1.0** artifact. The connector is Apache-2.0 code here; the geometry it
produces is a derived database and inherits share-alike terms.

**Not open:** trained model weights; the hosted service; operator workflow and
incident-management surface; customer-specific integrations.

The line is drawn so everything needed to reproduce and check a published result is open,
and everything that is an operational product is not.

## Architecture

Go for the platform, Rust for the numerical core, TypeScript and React for the client. The
platform is a graph of self-contained extensions resolved at startup through a
dependency-injection container, built on the [Forge](https://github.com/xraph/forge)
composition model with [Grove](https://github.com/xraph/grove) for storage and
[Fabriq](https://github.com/xraph/fabriq) as the data fabric.

```
blackstart/
├── cmd/          # blackstart (host), bsctl (CLI)
├── pkg/          # shared primitives: quantities, bitemporality, identifiers
├── extensions/   # the unit of work — one directory per extension
├── rust/         # Cargo workspace: the numerical core
├── web/          # pnpm workspace: the 3D client
├── proto/        # gRPC contract between Go and Rust
└── data/         # public test cases, golden files, sample files
```

Electrical quantities are typed values carrying magnitude, unit, and per-unit versus SI
reference base, never bare floats. Mixing the two is the classic power-systems bug class,
and the type system is arranged to make it hard.

Decisions are recorded in [`docs/adr`](docs/adr). Extension guides:
[`weather`](docs/extensions/weather.md), [`hazard`](docs/extensions/hazard.md).

## Status

| Phase | Scope | State |
|---|---|---|
| 0 | Foundation — host, DI, migrations, provenance, audit, CI | **complete** |
| 1 | Network model — topology, geospatial routing, importers, viewer | **complete** |
| 2 | Solver — Rust AC/DC power flow, N-1, checked against pandapower | **complete** |
| 2.5 | IEC 61850 SCL parser | **complete** |
| 3 | Weather and hazard — HRRR ingest, GRIB2 decoding, per-span ratings | **complete** |
| 4 | Cascade — propagation, scenario orchestration, blast-radius geometry | in progress |
| 5–8 | Grid protocols, prediction, full visual system | fragility landed 8 Aug 2026; rest not started |

The operator console renders a hazard layer over the network with quantized severity
colouring, covering 216 rated spans.

## Building

Requires Go 1.26+, Rust (see `rust/rust-toolchain.toml`), Node 22+ with pnpm, and Docker
for the integration suite.

```bash
make setup      # install git hooks and development tooling
make dev-up     # start Postgres+PostGIS, Redis, FalkorDB
make lint test  # what CI runs
```

Three services are required rather than optional, because every entity write goes through
the data fabric: Postgres is the source of truth, Redis drains the transactional outbox,
and FalkorDB holds the graph projection. Host ports are deliberately non-default so the
stack does not collide with a Postgres or Redis already running.

```bash
export BLACKSTART_DATABASE_DSN='postgres://blackstart:blackstart@localhost:55432/blackstart?sslmode=disable'
export BLACKSTART_REDIS_ADDR=localhost:56379
export BLACKSTART_FALKORDB_ADDR=localhost:56380

make build-go
./bin/blackstart                 # the host; /health, /livez, /readyz
./bin/bsctl entities             # what the system models
./bin/bsctl version              # build identity and whether it is reproducible

make test-integration            # containerized, needs Docker
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Small, semantically scoped commits in
[Conventional Commits](https://www.conventionalcommits.org/) form, never squashed.

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
