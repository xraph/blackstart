# `hazard`

Turns weather into engineering quantities a conductor cares about: what the
wind pushes with, what the ice weighs, and how much current the line can carry
before it sags into something.

The platform's **first derived quantity**, which makes it the first thing here
to complete ADR-0006's chain end to end. A rating traces to the run that
produced it, the run to the model version and the code commit behind it, and
through the weather grids to the dataset versions and the sources they were
fetched from.

## What it computes

| Quantity | Unit | Model |
|---|---|---|
| `wind_load` | N/m | drag on a cylinder, `C_d = 1.0` |
| `ice_thickness` | m | Jones (1998) equivalent radial glaze |
| `ice_load` | kg/m | the same sleeve, as mass |
| `combined_load` | N/m | drag and ice weight as a vector |
| `ampacity` | A | IEEE Std 738 steady-state heat balance |

All per span, per valid time, per forecast issuance.

## The chain

```
a sampled weather cycle
  ▼
span → route → branch → conductor assignment       geo.Route carries BranchID
  ▼
read the fields each model needs, per span, per valid time
  ▼
ice → wind → combined · thermal
  ▼
BulkWrite values · Computation row · provenance Run
```

**Ice runs before wind** because the wind model needs the accreted thickness to
know how wide a face it is pushing on.

## Nothing here guesses a conductor

IEEE 738 needs a diameter, two resistance points, emissivity, absorptivity and
a temperature limit. Nothing else in this platform has any of them — the
network model carries a single static rating per branch — so `hazard` ships a
library of standard ACSR types and a per-branch `Assignment` binding one to a
line.

Every assignment carries a **basis**:

| Basis | Meaning |
|---|---|
| `surveyed` | somebody looked |
| `assumed_by_voltage` | nobody looked; a conductor typical of the voltage class |

A span whose branch has no assignment is **counted unrated and produces
nothing**. It is never given a default. A rating conditioned on a guessed
conductor looks exactly like one conditioned on a surveyed one, and the whole
point of recording the basis is that they must not.

`AssumeByVoltage`'s thresholds — under 200 kV `hawk`, 200–299 kV `drake`,
300 kV and above `cardinal` — are defensible starting points from typical
transmission practice, not authority. They deserve a second opinion from
somebody who strings line for a living.

## Three things that are easy to get wrong

**The gust and the mean are not interchangeable.** The mechanical models take
the *gust*, because drag goes as the square of speed and a conductor fails on
the peak. The thermal model takes the *mean*, because cooling integrates over
the ten to twenty minutes of a conductor's thermal time constant — a
three-second gust does not cool a conductor, and it breaks one instantly if it
is going to.

**Meteorological wind direction names where the wind comes *from*.** The
components `u` and `v` say where the air is going, so resolving a crosswind
means `atan2(-u, -v)`, with `u` before `v` to convert
counterclockwise-from-east into clockwise-from-north. A span's bearing is a
line rather than an arrow, and so is the wind's for this purpose: both fold
into a 0–90° incidence, so a span surveyed south-to-north loads like the same
span surveyed north-to-south.

**Absence is absence.** GFS publishes no freezing-rain accumulation, so a
GFS-derived pass has **no ice series at all** — not a zero one. Inventing an
accumulation would put a fabricated load on a line nobody needs to derate,
which is the expensive direction to be wrong in. A moment with no temperature
produces nothing rather than part of the set, because temperature enters every
model here and a span with a wind load and no rating would be a gap nothing
downstream could reconstruct the reason for.

## The external reference

IEEE 738-2012's own worked example is the test §10 requires — the same role
ecCodes plays for the GRIB decoder. Drake at 100 °C in a 40 °C ambient,
0.61 m/s across the conductor, clear sky at 30° N on 10 June at 11:00 solar.

This model gives **990 A**. Every published intermediate agrees to three
digits: Reynolds 864, solar altitude 74.89°, clear-sky flux 1027 W/m²,
resistance 9.3905×10⁻⁵ Ω/m, convection 82.07 W/m, radiation 24.44 W/m. The
three heat terms are asserted individually, because a rating that lands inside
tolerance with two terms wrong in opposite directions is not agreement.

Two departures from the standard, both conservative and both documented in
place: the sun is assumed square to the conductor (a rating is per branch while
bearings are per span), and cloud attenuates the solar flux via Kasten and
Czeplak — the standard rates for clear sky by convention, and blackstart rates
against a forecast that has to be allowed to say overcast.

## Storage

Two relational tables and one hypertable.

| Table | Holds |
|---|---|
| `blackstart_hazard_assignment` | one conductor per branch, unique on `(tenant_id, branch_id)` |
| `blackstart_hazard_computation` | one row per pass: cycle, run, coverage counts |
| `blackstart_hazard_span_value` | the numbers, keyed `span｜quantity｜issuance` |

The series table carries exactly `(time, tenant_id, tag_id, value, quality,
scope_id)` — fabriq's Timescale adapter names those six in its INSERT and
nothing else. It has no row-level security, which is the documented exception
rather than an oversight: Timescale's columnstore forbids it. Every read
carries a tenant predicate written by hand, in one place, for that reason.

Several passes over one cycle is the normal case — one per correction to the
assignments — and every one is kept, so a rating already consumed downstream
still has the computation that made it.

## API

```
GET /api/v1/hazard/conductors
GET /api/v1/hazard/assignments
GET /api/v1/hazard/computations
GET /api/v1/hazard/spans/:id/rating?issued=&from=&to=
GET /api/v1/hazard/spans/:id/loading?issued=&from=&to=
```

Read-only: a hazard value is produced by a computation that records its
provenance, and an endpoint accepting one inline would produce a number nothing
could trace.

`issued`, `from` and `to` are all **required**. A window this API chose would
give the same request a different answer tomorrow — and "what is the rating
now" and "what did the 06Z run say it would be" are different questions, only
the second of which can be checked afterwards.

## CLI

```bash
bsctl hazard conductors                                    # the library
bsctl hazard assign --branch <id> --conductor drake --basis surveyed
bsctl hazard assign --branch <id> --kv 345                 # assumed, basis stamped
bsctl hazard compute --cycle wxcyc_…
bsctl hazard rating  --span span_… --issued 2026-08-04T01:00:00Z
bsctl hazard loading --span span_… --issued 2026-08-04T01:00:00Z
```

`conductors` needs no database — the library is compiled in.

`assign` takes either `--conductor` or `--kv`. Giving the voltage picks a
conductor *and* stamps the basis as assumed, so the two cannot drift apart.
Giving neither is refused rather than defaulted.

## The worker

The server runs it and `bsctl` does not. It needs no registry slugs — it reads
what `weather` already stored — so draining workers is the whole condition.

Every two minutes it looks for cycles that have been sampled and have no
computation, inside a twelve-hour lookback, and rates up to four of them. The
queue is durable state rather than an event: a cycle is pending exactly when it
is sampled and unrated. That survives a restart, works across replicas, and
cannot drop work the way an in-process notification can — which matters here,
because `*hooks.Bus` is never provided into the container, so an event-driven
trigger would have been dead on arrival.

A cycle that already has a computation is skipped. Recomputing after a conductor
assignment changes is a deliberate act through `bsctl hazard compute`: ratings
already consumed downstream should not move because a worker decided to try
again.

Leader-elected on PostgreSQL advisory lock **20003**. Two replicas rating one
cycle would each write a computation and a provenance run, leaving two answers
for one forecast with nothing to say which to believe.

## Live behavior

Against the dev stack, one traced route (21 spans) assigned Drake and an HRRR
cycle sampled onto it:

```
computation hzcmp_01kz5jdcz0fknach2ks9f0pbdx
run         run_01kz5jdcxtfkn9c3mm7kk5xh87
spans       21
unrated     917
values      105
covered     21 of 21 span-hours
```

917 spans on unassigned branches produced nothing, and the command says so with
the remedy attached. The run names the `bs-hazard` model version and the 19
dataset versions behind the cycle's grids.

One span at 35.6 °C with a 4.3 m/s wind rated **1715 A** — about 1.7× Drake's
static summer rating, which is the whole value proposition of a dynamic rating
and is what a real wind against an assumed 0.61 m/s produces.

## Known gaps

- The conductor library belongs to an `asset` extension that does not exist
  yet. When it does, the library moves there and the entries migrate rather
  than being re-minted, which is why they already have identifiers.
- `AssumeByVoltage`'s thresholds want review by a transmission engineer.
- The solar term assumes the sun is square to the conductor. Resolving it
  against a bearing needs a rating that is per span rather than per branch.

## Forecast vintage changes the rating

Measured 9 August 2026, on 216 rated spans over HRRR cycles from 3 to 8
August. For each span and valid time the rating is computed twice: once from
the forecast issued some hours earlier, and once from the analysis issued at
that valid time. Same span, same conductor, same model, same code. Only the
weather vintage differs.

| Lead | Span-hours | Mean | Median | p95 | Max | >5% | >100 A |
|---|---|---|---|---|---|---|---|
| 1 h | 9,993 | 62.3 A | 49.7 A | 170.2 A | 500.6 A | 45.4% | 19.5% |
| 2 h | 9,129 | 87.6 A | 62.0 A | 266.5 A | 748.1 A | 54.2% | 27.1% |
| 3 h | 8,265 | 80.0 A | 68.2 A | 205.1 A | 572.2 A | 57.0% | 29.0% |
| 6 h | 6,774 | 87.7 A | 71.4 A | 212.7 A | 505.2 A | 55.7% | 38.1% |
| 12 h | 3,276 | 120.7 A | 102.2 A | 295.8 A | 572.1 A | 69.2% | 50.8% |
| 18 h | 1,632 | 97.1 A | 88.9 A | 237.6 A | 427.7 A | 60.9% | 43.0% |

Mean rating over the period is 1,181 A, so the twelve-hour row is half of all
span-hours moving by roughly a tenth of the rating. Most assignments in this
run are `assumed_by_voltage`, which conditions the absolute ratings on a
guess. It does not condition the difference: both runs read the same assumed
conductor, so the assumption cancels.

This is divergence rather than error. The analysis is a model product, not a
measurement, and this period contains no icing at all, so it exercises the
wind and thermal paths only.

Reproduce it against a database that has hazard values for overlapping
issuances:

```sql
WITH v AS (
  SELECT split_part(tag_id,'|',1) AS span,
         to_timestamp(substr(split_part(tag_id,'|',3),1,8)
                      ||substr(split_part(tag_id,'|',3),10,4),
                      'YYYYMMDDHH24MI') AS issued,
         time AS valid, value
  FROM blackstart_hazard_span_value
  WHERE split_part(tag_id,'|',2) = 'ampacity'
)
SELECT (EXTRACT(EPOCH FROM (f.valid-f.issued))/3600)::int AS lead_h,
       count(*), avg(abs(f.value-a.value)), max(abs(f.value-a.value))
FROM v f JOIN v a
  ON a.span = f.span AND a.valid = f.valid AND a.issued = a.valid
WHERE f.issued < f.valid
GROUP BY 1 ORDER BY 1;
```

The join condition `a.issued = a.valid` is what picks the analysis: a run
whose issuance and valid time coincide is the zero-hour field.
