# `weather`

Numerical weather prediction ingest. Retrieves NOAA HRRR and GFS products,
records the provenance of every retrieval, and turns gridded fields into
**per-span samples** the `hazard` extension conditions on.

It computes nothing meteorological. Wind loading, ice accretion and IEEE 738
dynamic line rating belong to `hazard`; this extension's job ends at "here is
the gust at this span at this valid time, from this retrieval".

## Sources

Only what the §8 registry names.

| Source | Feed | Format | Cadence |
|---|---|---|---|
| NOAA HRRR | `hrrr` | GRIB2 | hourly, 3 km, CONUS only |
| NOAA GFS | `gfs` | GRIB2 | 00/06/12/18Z, 0.25°, global |

The engine takes a **feed slug, never a URL**. Resolving through `datasource`
is the only route to a fetch, so a source outside §8 cannot be ingested
without first being added to the registry — which is a decision with a person
attached rather than a convention someone remembers.

## How a retrieval works

```
resolve feed (datasource)
  ▼
fetch the .idx sidecar        10 KB describing a 151 MB product
  ▼
HTTP Range per wanted field   a few MB instead of 151 MB
  ▼
sha256 → PutBlob → RecordDataset
  ▼
Grid row naming the bytes and the dataset version
```

Then, separately:

```
read the blob → PutGrid (once) → SampleGrid (every span at once) → BulkWrite
```

Retrieval and sampling are separate operations because they fail and recover
differently. A retrieval is irreplaceable once NOAA's rolling archive passes
it; sampling is recomputable from the stored bytes forever.

## Two things that are easy to get wrong

**Issuance and valid time are independent.** A forecast says "at 09:00 we
expected 14:00 to look like this". Every sample carries both, and the series
key is where issuance lives — the Timeseries port has one string key and one
float per point, with no column for it. Collapsing the two axes is the most
common source of silent leakage in the outage-prediction literature.

**Absence is absence.** HRRR covers CONUS and nothing else. A span outside it
gets *no sample*, not a zero — a zero gust is a real value, and a fabricated
calm entering a hazard calculation is invisible once it is there. The `covered`
and `total` counts on each `Grid` row are where that stays visible.

## Wind is stored as components

`wind_u_10m` and `wind_v_10m`, not a scalar speed. Neither product publishes an
instantaneous scalar wind — but more importantly, a crosswind on a span is the
component perpendicular to it, which is why `geo` stores an azimuth per span.
A scalar speed cannot produce one.

## Entities

| Entity | Prefix | What |
|---|---|---|
| Cycle | `wxcyc` | One run of one model: feed, model, issuance, status |
| Grid | `wxgrd` | One retrieved field-hour: blob reference, dataset version, coverage |
| Station | `wxstn` | A surface observing site, with a point geometry |

Samples live in two Timescale hypertables rather than as entities:
`blackstart_weather_span_sample`, keyed `span|field|issuance`, and
`blackstart_weather_station_obs`, keyed `station|field`. Those tables sit
outside row-level security — Timescale's columnstore forbids it — so tenant
isolation there is enforced by hand in one store method.

## Command line

```bash
bsctl weather ingest --feed hrrr --cycle 2026-08-03T12:00:00Z --hours 1-6
bsctl weather sample --cycle wxcyc_...
bsctl weather cycles
bsctl weather show --span span_... --issued 2026-08-03T12:00:00Z --field gust10m
```

## HTTP

```
GET /api/v1/weather/cycles
GET /api/v1/weather/cycles/:id
GET /api/v1/weather/cycles/:id/grids
GET /api/v1/weather/spans/:id/forecast?field=&issued=&from=&to=
GET /api/v1/weather/stations/:id/observations?field=&from=&to=
```

Read-only. Samples arrive through the ingest pipeline where their provenance
is recorded; an endpoint accepting them inline would produce numbers nothing
could trace.

`issued` is required on the span forecast rather than inferred from the
window, because "what does the latest run say" and "what did the 06Z run say"
are different questions — and a reader who cannot ask the second cannot check
a forecast against what actually happened.

## The worker

Disabled by default; the server runs it and `bsctl` does not. Each tick
ingests the newest cycle, then walks back to a configured horizon (24 hours by
default), skipping cycles already recorded. Leader-elected on PostgreSQL
advisory lock **20002**, so N replicas cooperate rather than each polling NOAA
for the same cycle.

## Decoding

GRIB2 decoding is `rust/bs-grib`, served over gRPC from the solver binary
under `blackstart.weather.v1`. It implements grid templates 3.0 and 3.30 and
data templates 5.0 and 5.3 — the ones HRRR and GFS actually use, verified
against real messages. Anything else is an error naming the template rather
than a best-effort parse, because a misread packing produces plausible wrong
numbers rather than a crash.

Its output is checked against **ecCodes**, ECMWF's own GRIB library, run out of
process with the goldens committed. See `rust/bs-grib/tests/oracle.rs`.

## Related

- Design: `a private design note`
- Plans: `a private design note,decoder,ingest}.md`
- ADR 0006 (provenance), spec §2.1 (bitemporality), §8 (the registry)
