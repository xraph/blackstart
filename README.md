# Blackstart

Weather-driven outage risk for United States electric power networks.

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Blackstart takes a public weather forecast, works out what it does to each individual
conductor span, and estimates how likely every component of a transmission network is to
fail. It is a digital twin in a narrow and specific sense: it keeps a live, versioned model
of the network, and it will answer questions about that model as of any past moment rather
than only as of now.

It's early, and the honest summary is short. The network model, the Rust solver, the
weather-to-rating path and the fragility layer all run. Cascade simulation mostly runs.
But nothing has been calibrated against an observed outage record and there is no backtest
yet, which means the system cannot tell you how accurate it is, and neither can I. There
are no releases. Don't rely on it for anything.

## Running it

You'll need Go 1.26+, a Rust toolchain (see `rust/rust-toolchain.toml`), Node 22+ with
pnpm, and Docker.

```bash
make setup      # git hooks and tooling
make dev-up     # Postgres+PostGIS, Redis, FalkorDB
make lint test  # what CI runs
```

All three services are required rather than optional, because every entity write goes
through the data fabric: Postgres is the source of truth, Redis drains the transactional
outbox, and FalkorDB holds the graph projection. The host ports are deliberately odd so
the stack will not collide with a Postgres or Redis you already have running.

```bash
export BLACKSTART_DATABASE_DSN='postgres://blackstart:blackstart@localhost:55432/blackstart?sslmode=disable'
export BLACKSTART_REDIS_ADDR=localhost:56379
export BLACKSTART_FALKORDB_ADDR=localhost:56380

make build-go
./bin/blackstart      # the host; /health, /livez, /readyz
./bin/bsctl entities  # what the system models
./bin/bsctl version   # build identity, and whether it is reproducible

make test-integration # containerized, needs Docker
```

If you want to see the whole thing work, `bsctl` walks the pipeline in the order it
actually runs: `model` imports a network, `geo` routes it over real terrain, `weather`
retrieves and samples a forecast cycle, `hazard` turns those samples into mechanical
loading and thermal ratings, `risk` turns those into per-component failure probabilities,
and `solve` and `n1` drive the solver.

## Layout

Go for the platform, Rust for the numerical core, TypeScript and React for the client. The
platform is a graph of self-contained extensions wired together at startup through a
dependency-injection container, built on [Forge](https://github.com/xraph/forge) with
[Grove](https://github.com/xraph/grove) for storage and
[Fabriq](https://github.com/xraph/fabriq) as the data fabric.

```
cmd/          blackstart (host), bsctl (CLI)
pkg/          shared primitives: quantities, bitemporality, identifiers
extensions/   the unit of work, one directory per extension
rust/         Cargo workspace: the numerical core
web/          pnpm workspace: the 3D client
proto/        gRPC contract between Go and Rust
data/         public test cases, golden files, samples
```

At commit `c3c93c2b` that is 15 extensions, 496 Go files and 12 Rust crates, roughly 99,500
and 20,500 lines respectively, with 1,326 Go tests and 312 Rust tests.

## Two decisions that explain most of the rest

The first is that electrical quantities are typed values carrying a magnitude, a unit, and
a per-unit-versus-SI reference base, and never bare floats. Mixing per-unit and physical
units is the classic power-systems bug, the kind that gives you an answer wrong by a
factor rather than an answer that is obviously broken, so the type system is arranged to
make that mistake awkward to write.

The second is that every fact carries two timestamps, one for when it was true of the
world and another for when this system found out about it. That second axis is the unusual
one, and it means a model you evaluate as of last Tuesday reads what was known last
Tuesday rather than the outage record somebody corrected on Thursday. Nearly every
backtest in this field is quietly contaminated by that distinction, which is why it had to
land in the first few weeks instead of being retrofitted later. The reasoning is in
[`docs/adr/0006-provenance.md`](docs/adr/0006-provenance.md).

## What's checked, and what isn't

The power flow agrees with committed pandapower reference solutions on the IEEE 14, 30,
57, 118 and 300 bus cases and on PEGASE 2869, inside per-quantity tolerances that each
carry a written justification. If a golden comparison fails then the solver is wrong, and
the tolerance does not move.

The GRIB2 decoder is checked against ecCodes on three real NOAA messages. That test exists
because every other test in the crate compares the decoder against itself or against a
plausibility band, and a decode that is wrong by a constant factor, or transposed, or
shifted by one row, will sail straight through all of them.

There are invariant tests as well, covering power balance, nodal balance and linearity,
which hold regardless of what any reference tool says. They are there to catch the one case
a golden file cannot: a solver and its own reference being wrong in the same direction.

The cascade engine is not checked against anything external. No historical cascade has been
reproduced and no simulated footprint has been compared against an observed one, so read
its output as a demonstration of mechanism rather than as a prediction.

One measurement is worth pulling out here. If you compute a line rating from the forecast
an operator actually held, rather than from the analysis that only exists afterwards, the
answer changes on most span-hours: at twelve hours of lead, half of them move by more than
100 A against a mean rating of 1,181 A. The method, the numbers and the caveats are all in
[`docs/extensions/hazard.md`](docs/extensions/hazard.md).

## What's new here, and what isn't

Very little of the method is new, and it is worth saying so plainly. Coupling
weather-derived component fragility to a network model to estimate consequence was
published by Panteli, Pickering, Wilkinson, Dawson and Mancarella back in 2017, and the AC
cascading failure model was later extended to weather events at hourly intervals by
Noebels, Preece and Panteli. Predicting outages from weather goes back to 2014 and is
deployed at large utilities today. Rare-event methods for blackout probability are Shortle
and colleagues. Influence-graph cascade models are Dobson, Hines and others. Grid digital
twins are a category you can go and buy, and versioned network models with audit trails
are ordinary utility practice.

What all that work produces is studies, meaning a method run once to get a result about a
system at a particular moment. This is an attempt at the thing underneath one: maintained
rather than assembled per analysis, queryable as of a past instant, running on data anyone
can download, under a licence that requires no transaction with me. Whether that is worth
doing is a fair question to ask, and the honest answer is that it matters most to the
rural cooperatives and municipal utilities who cannot commission this kind of analysis at
all.

## Advisory only

Blackstart produces forecasts, rankings and recommendations. It does not act on them.

Nothing here writes to a SCADA, EMS, ADMS, RTU, relay or any other field device, and no
endpoint will accept an instruction bound for one. There are no protocol clients yet at
all, so no MMS, GOOSE, DNP3 or IEC 60870-5-104. The IEC 61850 support that does exist is
an SCL parser, which reads configuration files and originates nothing.

When those clients do get written they will be read-only, and the lint in the required CI
check already covers the packages they will live in. A system that ranks contingencies by
probability is a short step away from acting on them, so the line sits at the repository
boundary rather than at a configuration flag, where you can check it yourself.
[`docs/SAFETY.md`](docs/SAFETY.md) is the normative version.

## Data

Everything the system consumes can be downloaded by anyone, with no NDA, no licence
negotiation and no utility relationship. That is a constraint rather than a convenience,
because it means a result can be reproduced by somebody who does not work at a utility.
Every ingest records the source URL, the retrieval time, a content hash and the parser
version.

| Source | Provides |
|---|---|
| [Texas A&M ACTIVSg synthetic grids](https://electricgrids.engr.tamu.edu) | Realistic synthetic networks, free of CEII restriction |
| IEEE standard test cases (14/30/57/118/300) | Solver validation |
| NOAA HRRR, NBM, NDFD, GFS, GEFS, HREF | Numerical weather prediction (GRIB2) |
| NOAA MRMS | Radar-derived precipitation and hail |
| NWS ASOS/METAR | Surface observations |
| [ORNL EAGLE-I](https://eagle-i.doe.gov) | County-level historical outage counts |
| [DOE OE-417](https://doe417.pnnl.gov) | Major electric disturbance reports |
| EIA-860 / 861 / 930 | Generator inventory, reliability metrics, hourly BA operations |
| NERC TADS / DADS | Transmission and distribution outage statistics, aggregate only |
| [EIA U.S. Energy Atlas](https://atlas.eia.gov) | Transmission line geospatial layer, 69–765 kV |
| [PNNL-33587](https://www.pnnl.gov/main/publications/external/technical_reports/PNNL-33587.pdf) | Fragility functions, and the literature it catalogues |
| [OpenStreetMap power layer](https://overpass-api.de) | Per-structure geometry (ODbL 1.0, share-alike) |
| USGS 3DEP | Terrain elevation |
| NLCD / LANDFIRE | Land cover |
| ENTSO-E CGMES conformity models | CIM/CGMES parser test data |
| Public IEC 61850 sample SCD/ICD files | SCL parser test data |

Results are published only for synthetic systems, never for a real named utility. The
ACTIVSg cases exist precisely so this kind of work can happen in the open without touching
Critical Energy/Electric Infrastructure Information, and while they are statistically and
geographically realistic they correspond to no real network. Contributions carrying
results for a named utility will not be accepted. Software vulnerabilities should go to
[SECURITY.md](SECURITY.md).

## Licensing

Apache-2.0 covers the simulation engine, the parsers and importers, the data connectors,
the grid domain model including the bitemporal and provenance machinery, the benchmark
suite and validation record, and the reference client.

OSM-derived route and structure geometry ships as a separate artifact under ODbL 1.0. The
connector that fetches it is Apache-2.0 code in this repository, but the geometry it
produces is a derived database and inherits OpenStreetMap's share-alike terms, so it has
to travel on its own.

Trained model weights, the hosted service, the operator workflow surface and any
customer-specific integration are not open. The line is drawn so that everything you would
need to reproduce and check a published result is here.

## More

[ROADMAP.md](ROADMAP.md) covers what is built, what comes next and why in that order, and
what will not be built at all. Design decisions are recorded in [`docs/adr`](docs/adr), and
the extension guides so far are [`weather`](docs/extensions/weather.md) and
[`hazard`](docs/extensions/hazard.md).

If you want to contribute, see [CONTRIBUTING.md](CONTRIBUTING.md). Small, semantically
scoped commits in [Conventional Commits](https://www.conventionalcommits.org/) form, never
squashed.

Apache License 2.0, see [LICENSE](LICENSE) and [NOTICE](NOTICE).
