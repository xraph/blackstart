# Blackstart

**Weather-driven outage risk and cascading failure impact for United States electric power systems.**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

> **Status: early development, Phase 3 (weather and hazard) complete.** The repository is
> being built in public from its first commit. The network model, the solver, and the
> weather-to-line-rating path work end to end; the prediction and visualization layers do
> not exist yet. See [Status](#status) for what actually exists today.

---

Blackstart estimates how likely each component of a power grid is to fail under a given
weather forecast, and what happens to the rest of the network when it does. It ingests
public numerical weather prediction, derives the engineering quantities that actually
break equipment — conductor wind loading, ice accretion, vegetation contact risk,
thermal ratings — and attaches them to a versioned, geographically-routed model of the
network. Weather is sampled along each conductor span rather than at a single point per
line, because a 30 km transmission line does not experience one gust.

Those per-component hazard exposures feed a calibrated failure-probability model, which
in turn ranks which contingencies are worth simulating. A physics-based simulator then
takes each candidate initiating outage and propagates it: protection operates, flow
redistributes, downstream elements overload and trip, the network separates into islands,
under-frequency and under-voltage load shedding fires, and each island either stabilises
or collapses. The output is a time-ordered event sequence, a final islanded state, and
the quantities an operator or planner would actually use — customers interrupted,
megawatts lost, expected unserved energy, and the geographic extent of the affected area.

Every number the system produces is traceable to a dataset version, a model or solver
version, and a code commit. Facts carry two time axes: when they were true of the world,
and when the system learned them. A model evaluated as of some past moment sees only what
was known at that moment, never the record corrected a week later. This is unglamorous
and it is the difference between a backtest that means something and one that does not.

## The technical claim

The contribution is a **coupling**: weather-conditioned per-component failure
probabilities feeding a physics-based cascade simulator, integrated end to end to produce
expected unserved energy and geographic impact extent.

Two research communities have solved the halves separately. One predicts outage counts
from weather with no network model, so it cannot say what the outage does to the system.
The other simulates cascade propagation with no weather, so it cannot say which
contingency is likely tonight. Blackstart connects them, so that the forecast determines
which contingencies get solved and the network model determines what those contingencies
cost.

**What this project does not claim.** Predicting outages from weather is published work
since 2014 and is operationally deployed at major utilities — that is not new here.
Rare-event and importance-sampling methods for blackout analysis were established in
2011–2012. Graph-neural-network and physics-informed surrogates for power flow have been
an active field since 2023. Influence-graph cascade models are Dobson, Hines, and
colleagues, 2012–2020. Blackstart builds on all of it. The coupling is the part that is
checkable and not already done, which is why it is the only claim made.

## Advisory only

**Blackstart produces forecasts, rankings, and recommendations. It does not act on them.**

There is no code path that writes to a SCADA, EMS, ADMS, or field device. Protocol
adapters — IEC 61850, DNP3, IEC 60870-5-104 — are **read-only clients**. There is no
control API, no write method on any protocol client, and no placeholder suggesting one is
planned. This is an architectural boundary enforced in continuous integration, not a
limitation of the current phase, and it will not be relaxed. See [docs/SAFETY.md](docs/SAFETY.md).

Recommendations — crew pre-positioning, candidate switching plans, load transfer options,
dynamic rating headroom — are presented for a human to evaluate and execute through their
own systems.

## Public data only

Blackstart runs entirely on data that anyone can download without an NDA, a license
negotiation, or a utility relationship. This is a deliberate constraint: it means the
results are reproducible by people who do not work at a utility.

| Source | Provides |
|---|---|
| [Texas A&M ACTIVSg synthetic grids](https://electricgrids.engr.tamu.edu) | Realistic synthetic networks (2000-bus Texas, 200, 500, 10k) free of CEII restriction |
| IEEE standard test cases (14/30/57/118/300) | Solver validation |
| NOAA HRRR, NBM, NDFD, GFS, GEFS, HREF | Numerical weather prediction (GRIB2) |
| NOAA MRMS | Radar-derived precipitation and hail |
| NWS ASOS/METAR | Surface observations |
| [ORNL EAGLE-I](https://eagle-i.doe.gov) | County-level historical outage counts |
| [DOE OE-417](https://doe417.pnnl.gov) | Major electric disturbance reports |
| EIA-860 / 861 / 930 | Generator inventory, reliability metrics, hourly BA operations |
| NERC TADS / DADS | Transmission and distribution outage statistics, aggregate reports only |
| [EIA U.S. Energy Atlas](https://atlas.eia.gov) | Transmission line geospatial layer, 69–765 kV |
| [PNNL-33587](https://www.pnnl.gov/main/publications/external/technical_reports/PNNL-33587.pdf) and the open-access fragility literature it catalogues | Published wind and ice fragility functions per structure class |
| [OpenStreetMap power layer](https://overpass-api.de) | Per-structure geometry — the only public source of structure positions (ODbL 1.0, share-alike) |
| USGS 3DEP | Terrain elevation |
| NLCD / LANDFIRE | Land cover and vegetation fuel |
| ENTSO-E CGMES conformity models | CIM/CGMES parser test data |
| Public IEC 61850 sample SCD/ICD files | SCL parser test data |

Every ingest records the source URL, retrieval timestamp, content hash, and the version
of the parser that read it.

## Security and responsible disclosure

**Vulnerability analysis results are published only for synthetic test systems, never for
real named utilities.**

The Texas A&M ACTIVSg cases exist precisely so that this research can be done in the open
without touching Critical Energy/Electric Infrastructure Information. They are
statistically and geographically realistic but correspond to no real utility's network.
All published cascade results, ranked contingency lists, and blast-radius figures in this
repository use them.

If you identify a security issue in the software, see [SECURITY.md](SECURITY.md).

## Open-core boundary

Stated plainly so there is no ambiguity about what this repository is.

**Open, Apache-2.0, in this repository:**

- The simulation engine — power flow, contingency analysis, cascade propagation
- All parsers and importers — IEC 61850 SCL, CIM/CGMES, MATPOWER, PSS/E, GRIB2
- All data connectors for the public sources listed above
- The grid domain model, including the bitemporal and provenance machinery
- The benchmark suite and the numerical validation record
- The reference web client

**Open, separately licensed:**

- OSM-derived route and structure geometry, as a standalone **ODbL 1.0** artifact. The
  connector that fetches it is Apache-2.0 code in this repository; the geometry it
  produces is a derived database and inherits OpenStreetMap's share-alike terms, so it
  ships on its own rather than inside the Apache-2.0 release.

**Not open:**

- Trained model weights
- The hosted service
- Operator workflow and incident-management product surface
- Customer-specific integrations

The boundary is drawn so that everything needed to reproduce and check a published result
is open, and everything that is an operational product is not.

## Architecture

Go for the platform, Rust for the numerical solver, TypeScript and React for the client.
The platform is a graph of self-contained extensions resolved at startup through a
dependency-injection container, built on the [Forge](https://github.com/xraph/forge)
composition model with [Grove](https://github.com/xraph/grove) for storage and
[Fabriq](https://github.com/xraph/fabriq) as the data fabric.

```
blackstart/
├── cmd/          # blackstart (host), bsctl (CLI)
├── pkg/          # shared primitives: quantities, bitemporality, identifiers
├── extensions/   # the unit of work — one directory per extension
├── rust/         # Cargo workspace: the numerical solver
├── web/          # pnpm workspace: the 3D client
├── proto/        # gRPC contract between Go and Rust
└── data/         # public test cases, golden files, sample files
```

Electrical quantities are typed values carrying magnitude, unit, and per-unit-versus-SI
reference base — never bare floats. Mixing per-unit and physical units is the classic
power-systems bug class, and the type system is arranged to make it hard.

## Status

Phase 3 of nine, complete. Phase 4 next.

What works today, end to end against a live stack: a synthetic Texas network imported
from MATPOWER and routed over real geography; AC and DC power flow and N-1 contingency
analysis in the Rust solver, checked against committed pandapower reference solutions;
NOAA HRRR retrieval by byte-range against the GRIB2 index sidecars, decoded by a
first-party decoder verified against ecCodes, and sampled onto individual conductor
spans; and per-span wind loading, ice accretion and IEEE 738 dynamic line ratings
derived from those samples. Every number traces back through a provenance run to the
model version, the code commit, and the dataset versions it was computed from.

What does not exist yet: everything downstream of a rating. There is no cascade
simulation, no outage-history ingest, no risk model, no backtest harness, and the viewer
renders the network but carries no weather or hazard layer — so the Phase 3 demonstrable
is half-met, with ratings queryable through the API and CLI but not yet drawn on a map.

| Phase | Scope | State |
|---|---|---|
| 0 | Foundation — host, DI, migrations, provenance, audit, CI | **complete** |
| 1 | Network model — topology, geospatial routing, importers, viewer | **complete** |
| 2 | Solver — Rust AC/DC power flow, N-1, checked against pandapower reference solutions | **complete** |
| 2.5 | IEC 61850 SCL parser | **complete** |
| 3 | Weather and hazard — HRRR ingest, GRIB2 decoding, per-span wind, ice and IEEE 738 ratings | **complete** |
| 4 | Cascade — propagation, scenario orchestration, blast-radius geometry | in progress |
| 5–8 | Grid protocols, prediction, full visual system | not started |

Nothing here is ready to be relied upon. There are no releases yet.

Extension guides, as they land: [`weather`](docs/extensions/weather.md),
[`hazard`](docs/extensions/hazard.md).

## Building

Requires Go 1.26+, Rust (see `rust/rust-toolchain.toml`), Node 22+ with pnpm, and Docker
for the integration suite.

```bash
make setup      # install git hooks and development tooling
make dev-up     # start Postgres+PostGIS, Redis, FalkorDB
make lint test  # what CI runs
```

Three services are required rather than optional, because every entity write goes
through the data fabric: Postgres is the source of truth, Redis drains the transactional
outbox, and FalkorDB holds the graph projection. Host ports are deliberately non-default
so the stack does not collide with a Postgres or Redis you already have running.

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

See [CONTRIBUTING.md](CONTRIBUTING.md). The commit history is treated as an artifact of
this project: small, semantically-scoped commits in
[Conventional Commits](https://www.conventionalcommits.org/) form, never squashed, never
rewritten.

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
