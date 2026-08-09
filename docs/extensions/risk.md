# `risk`

Turns conductor loading into per-component failure probability: what
`hazard` computed as a wind or ice load on a span, `risk` multiplies through
a fragility curve to get P(fail) for the conductor strung on that span and
P(fail) for each structure carrying it.

The platform's **second derived quantity**, continuing ADR-0006's chain one
link further. A probability traces to the run that produced it, the run to
the model version and commit behind it, and through `hazard`'s own values to
the weather grids and datasets underneath those.

## What it computes

| Quantity | Unit | Scales with | Model |
|---|---|---|---|
| `conductor_failure` | probability, dimensionless | span length | exponential aggregation of a monotone rate, §below |
| `structure_failure` | probability, dimensionless | lognormal fragility curve, cited per class |

Both per component, per valid hour, per forecast issuance. The same three
axes `hazard`'s own values carry.

**These are two quantities describing different components, not two ways of
computing one number.** A conductor fails *between* two structures at a rate
proportional to how much of it is exposed to the weather, so its probability
scales with the span's length: a longer span sees more wire in the wind and
is more likely to lose some of it. A structure fails *at a point*, according
to what it is built like. A lattice tower and a wood pole do not fail the
same way regardless of how far apart they stand. So its probability carries
no length term at all. Storing them under one column would force a choice
about which structure a shared length belongs to, and there is no right
answer to that; `store.Quantity` keeps them as two named series instead.

## The chain

```
a weather cycle already sampled and rated by hazard
  ▼
span → route, structure → route                    geo.Route carries both
  ▼
read wind_load and ice_load hazard already wrote, per span, per valid hour
  ▼
span:      wind, ice, length      → ConductorProbability
structure: the higher of its two bounding spans' wind → Curve.Probability
  ▼
× the calibration scalar (1.0 today)
  ▼
PutBatch values · provenance Run
```

`risk` reads hazard's already-computed values rather than a forecast sample
or a grid directly. The only reason it depends on `weather` at all is to
turn the cycle a request names into the issuance that keys those values.

## The model is uncalibrated

Every probability this extension writes is multiplied by a scalar, and today
that scalar is `risk.ScalarUncalibrated`, which is `1.0`. A later piece fits
a global scalar against historical outage counts; until it does, `1.0` is
what every stored row carries, and every row carries it explicitly 
`ComponentValue.Scalar` rides along with the probability rather than being
applied and discarded, so a figure computed now and one computed after
calibration can be told apart by reading the row instead of by trusting
which day it was written.

The consequence is not that the numbers are wrong. It is that the model's
**absolute** values are not yet checkable against anything. Two spans scored
`0.02` and `0.06` under this pass are correctly ordered: the second really
is riskier by the fragility literature's own reasoning. Whether `0.06` is
the right probability for a real transmission structure in a real storm is a
question only calibration against real outages can answer, and until it
does, that answer does not exist. `bsctl risk compute` and `bsctl risk show`
both print the scalar with an explicit `(uncalibrated)` label for exactly
this reason. A risk figure that does not say whether it was calibrated
invites being read as though it already were.

## The curves are transferred, not local

Both structure curves in the `fragility` package library are fitted to
structures somewhere other than this platform's network: a 220 kV lattice
tower design in Liu & Yan (2022), a Class 3 Southern Yellow Pine pole in
Mohammadi Darestani & Shafieezadeh (2019). Neither paper measured a
blackstart-network structure, and nothing in this package adjusts for that 
the median and dispersion are read from the cited figure or table and stored
as given.

Both are stated in the source paper as a **wind speed**, not the wind load
this platform's intensity measure is. `fragility.windLoadFromSpeed` converts
each published median through the same drag equation `hazard`'s own wind
model uses, against a reference conductor (ACSR Drake, the platform's
existing worked example, not a value picked to make the conversion land
somewhere convenient). Because that conversion is quadratic, not linear, the
published dispersion has to be doubled to describe the load-space curve
correctly. A lognormal random variable pushed through a power-law map of
exponent *p* is still lognormal, with its log-scale spread multiplied by
*p*; `curve.go`'s comments carry the derivation beside each constant it
produces.

That conversion is not, however, a conversion into the same quantity
`compute.go` evaluates the curve against, and this is a real limitation, not
a rounding error. `windLoadFromSpeed` builds its load from the raw gust speed
against one fixed reference diameter (Drake, 0.02814 m). What `compute.go`
actually hands `Curve.Probability` is hazard's `wind_load`, computed as
`½·ρ(T)·Cd·(D_span + 2·t_ice)·V⊥²` where `V⊥ = gust × |sin(angle between
wind and span azimuth)|` (`extensions/hazard/mechanical/wind.go`), a value
that depends on the span's bearing relative to the wind and on whichever
conductor is actually strung on it, neither of which the curve's median was
built from. With the Lattice curve's β = 0.0794 giving it a near-step
transition at 15.85 N/m, both gaps dominate the answer: a 40 m/s gust 20°
off a span's line gives roughly 3.2 N/m and P≈0, while the same gust
perpendicular to the span gives roughly 27.6 N/m and P≈1. A tower's
collapse fragility is not a function of which way its line runs, but as fed
today, it is. The adjacent conductor's diameter varies 0.0218–0.0304 m
across `extensions/hazard/conductor`'s library, about 2.6β of swing on its
own. This is disclosed rather than corrected here, see Known gaps, because
re-deriving the azimuth and diameter dependence into the curve itself is a
model change, not a bookkeeping fix, and the calibration scalar this
extension already carries is the wrong tool for a directional error.

This transfer. A different structure population, in different weather, on
a different network, is exactly the error the calibration scalar exists to
absorb. A curve fitted to a Chinese 220 kV tower and a curve fitted to a
North American pole cannot both be exactly right for structures on this
platform's own lines, and the model does not pretend otherwise; it says so
by carrying an explicit, currently-unfitted scalar rather than by treating
the transferred median as ground truth.

## Components are treated as independent

Every probability `risk` writes is a marginal: P(this one component fails),
computed as if nothing else in the storm existed. Nothing in this extension
models the fact that one gust loading a span also loads its neighbors, or
that a structure and the spans it bounds share the same wind at the same
moment. That correlation is real. An ice storm that snaps one conductor
snaps the one next to it far more often than independence would predict 
and it is entirely absent here.

The consequence: **any P(N components fail) derived by combining these
marginals as though they were independent understates the tail.** Two spans
each scored at 5% do not fail together 0.25% of the time in a real storm;
they are both exposed to the same front, and the true joint probability is
higher, sometimes much higher, than the product of the marginals. Nothing
in this package corrects for that, and nothing should: modeling how failures
correlate under a shared weather event is a sampling problem over an entire
network state, not a property of one component's fragility curve. That
belongs to `cascade`'s sampling, which draws whole network states rather
than scoring components one at a time. A caller that wants a probability of
multiple simultaneous failures needs `cascade`, not an arithmetic combination
of `risk` values.

## The closed class set

`fragility.Class` has exactly two members: `Lattice` and `Pole`. That is not
an incomplete list waiting on more literature to be read. It is every class
this package found a citable fragility curve for. The search covered
PNNL-33587's own catalog and the open-access journal literature, and turned
up nothing with a numeric median and dispersion for a portal (H-frame)
structure. Rather than approximate one, `Portal` was never added to the
closed set at all: there is no `fragility.Portal` constant, so a structure
kind that would map to it, `"portal"`, is unrecognized by `ClassOf` for a
different reason than `"monopole"` or `"H-frame"` are. Those two lack a
curve; portal lacks a *class*.

This is not a rare gap. The project's own phase-3 survey measured
`power=portal` at roughly 4% of OpenStreetMap line-way nodes. A reader who
sees `Unclassed` land in that neighborhood on an OSM-imported route should
read it as this gap operating as designed, not as a bug to chase.

`Unclassed` and `Unhazarded` are counted separately and never summed,
because they are different failures. `Unclassed` is a gap in the **asset
record**, a structure whose `Kind` maps to no curve, whether because nobody
classified it or because no curve exists for what it is. `Unhazarded` is a
gap in the **weather coverage**, a span `hazard` never rated for this
cycle, so there is no loading to score a curve against. Only the first is
anybody's to fix by going out and surveying a structure; the second is fixed
by rerunning or widening `hazard`'s own pass, not by this extension or the
person who classified the asset.

Neither produces a stored zero. A structure with no curve and a structure
scored at 0% probability are different claims. One says "not evaluated,"
the other says "evaluated and found safe", and only the second belongs in
the value table.

## The higher of two spans, not the average

A structure typically bounds two spans. The one before it and the one
after it, by the route's ordinal sequence. When both have wind loading for
the same hour, `computeStructure` scores the structure against **the higher
of the two**, not their average. A structure is exposed to the worst of what
either adjacent span sees; a storm front rarely loads two consecutive spans
identically, and averaging would systematically understate the structure at
the edge of the front, where one span is deep in the worst wind and the
other has barely felt it yet.

## The conductor rate's shape is not itself a citation

`ConductorProbability` is `1 − exp(−λ·L·Δt)`, equation (1) of Jamieson,
Strbac and Bell (2020), reproduced exactly because it is open access and the
equation itself needs no adaptation. That aggregation form, turning a rate
into a probability over a span length and a duration, is the part of the
paper this package actually reproduces.

`λ` itself is not. Jamieson et al.'s own rate is empirical: bins of
historical fault counts indexed by wind speed, not a closed-form function,
and not published in the wind-load and ice-load units `hazard` publishes.
There is no `λ(wind_load, ice_load)` in that paper to transcribe.
`ConductorRate` in `fragility/conductor.go` is instead a **monotone
construction**, zero at zero load, smoothly rising, saturating at the order
of magnitude Jamieson's own curves report for the most exposed lines under
extreme wind. Its shape (an exponential approach to a ceiling, split into a
wind term and an ice term) is a choice this package made to have those
properties, not a curve fitted to any dataset. The comments beside its
constants say exactly this and should be read before this rate is quoted as
"from Jamieson et al.", only the aggregation equation is; the rate function
inside it is not.

## Storage

One hypertable.

| Table | Holds |
|---|---|
| `blackstart_risk_value` | one row per component, quantity, issuance and valid hour |

Keyed `(tenant_id, component_id, quantity, issued_at, valid_at)`, the same
shape as `hazard`'s own span-value table and forced row-level security for
the same reason: Timescale's columnstore forbids RLS, so this table must
never gain a compression policy, and nothing enforces that except this note
and the migration's own comment.

There is no computation table. `ComputeResult.ComputationID` is minted for a
caller to log, not stored anywhere this extension could resolve it back
from. The provenance run a pass records (`ComputeResult.RunID`) is the
traceable handle, through the existing provenance API.

## API

```
GET /api/v1/risk/components/:id/series?issued=&from=&to=
```

One endpoint, not split by component kind the way `hazard` splits rating
from loading: a `risk` component identifier already carries what it is in
its own prefix (`span_…` or `strc_…`), and a given component only ever has
values under the one quantity its compute pass wrote for it. `issued`,
`from` and `to` are all **required**, for the same reason hazard's span
endpoints require them. A window this API chose would give the same
request a different answer tomorrow.

Read-only: a probability is produced by a computation that records its
provenance, and an endpoint accepting one inline would produce a number
nothing could trace.

## CLI

```bash
bsctl risk compute --cycle wxcyc_…
bsctl risk show     span_… --issued 2026-08-04T01:00:00Z
bsctl risk show     strc_… --issued 2026-08-04T01:00:00Z
```

`compute` prints `spans`, `structures`, `unclassed` and `unhazarded` as
separate lines, with a plain-language remedy line under each nonzero
`unclassed`/`unhazarded` count, and prints the scalar through a label that
appends `(uncalibrated)` whenever it equals `risk.ScalarUncalibrated`.
`show` prints the same label on every row of the series, so a probability
copied out of one command's output never travels without the calibration
state it was computed under.

There is no worker. Unlike `hazard`, which rates a cycle automatically as
soon as it has been sampled, a `risk` pass runs only when `bsctl risk
compute` is invoked. Recomputing after a fragility curve or the calibration
scalar changes is a deliberate act for the same reason it is in `hazard`:
values already read by `cascade` should not move because something decided
to try again.

## Known gaps

- No curve exists for a portal (H-frame) structure. `Unclassed` on an OSM
  route will include roughly the 4% of nodes the phase-3 survey found
  tagged `power=portal`, and will stay that way until a citable fragility
  function for that class is found.
- The scalar is uncalibrated. Every value in the store today was computed
  under `1.0`; nothing downstream should treat these probabilities as
  checked against real outage history.
- Both structure curves are transferred from populations this platform did
  not measure. Calibration is the point at which that transfer error gets
  absorbed into a number instead of remaining an assumption.
- Components are scored independently. A joint probability across more than
  one component needs `cascade`, not an aggregation of `risk`'s own values.
- `ConductorRate`'s shape is this package's own construction, not a fit to
  Jamieson, Strbac and Bell (2020)'s data. Only its aggregation equation is
  reproduced from that paper.
- The structure curves are indexed on a quantity `compute.go` does not feed
  them. `fragility.windLoadFromSpeed` builds each curve's median from a raw
  gust speed against one fixed reference diameter; `compute.go` evaluates
  the curve against hazard's directional, span-specific `wind_load` instead
a value that depends on the angle between the wind and the span's
  azimuth and on the real conductor's diameter. With the Lattice curve's
  near-step transition at 15.85 N/m, both dependencies dominate: the same
  gust can read P≈0 or P≈1 depending only on which way the span runs, and
  the conductor library's diameter spread (0.0218–0.0304 m) is about 2.6β of
  swing by itself. A structure's collapse probability should not depend on
  its line's bearing or on which conductor happens to be strung on it, but
  as computed today, it does. See `fragility.windLoadFromSpeed`'s doc
  comment for the full derivation. Not corrected here. This is a model
  change, not a bookkeeping one, and the uncalibrated scalar is the wrong
  tool for a directional error.
