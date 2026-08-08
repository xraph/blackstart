# 0006 — Every number traceable to data, model and commit

**Status:** Accepted

## Context

The outage-prediction literature has a recurring reproducibility problem, and
much of it comes from a single mechanism: outage records are amended after the
fact. A model trained or evaluated against the corrected record has seen
information that did not exist at the time it claims to have made a prediction.
The resulting skill estimate is inflated, and the error is invisible in the
published result.

The same applies to weather. Forecasts are revised; a backtest that reads the
latest available field for a past timestamp is reading a better forecast than
the operator had.

Separately, a platform that produces a number an engineer might act on has to
be able to answer "why does this say what it says" mechanically, not by
investigation.

## Decision

Every figure the system produces is traceable to three things: the dataset
version that fed it, the model or solver version that computed it, and the code
commit that built the binary.

This is Phase 0 work, not a later addition. Specifically:

- Every dataset ingest records source URL, retrieval timestamp, content hash,
  and parser version.
- Every model version records training data version, feature set version,
  hyperparameters, code commit, and backtest results.
- Every simulation run records the scenario manifest, network model version,
  solver version, seed, and a hash of the result.
- `bsctl replay <run-id>` reproduces any run from its manifest alone.
- No result reaches the client without a provenance reference.

Bitemporality is the mechanism that makes the first problem impossible rather
than merely discouraged. Every fact carries **valid time** (when it was true of
the world) and **knowledge time** (when the system learned it). Stores expose
`AsOf(validTime, knowledgeTime)`, and a model evaluated as of time *T* resolves
every input at knowledge time *T*.

## Consequences

Bitemporality cannot be retrofitted. Adding it later invalidates every backtest
run before it, because those backtests cannot be shown not to have leaked. That
is why it lands in Phase 1 rather than alongside the prediction work that needs
it, and why the proving extension exercises it before any real entity exists.

Neither Grove nor Fabriq offers any help here — no `AsOf`, no valid-time or
knowledge-time support, nothing. The whole mechanism is ours.

There is a real cost in ergonomics. Every bitemporal entity carries four extra
columns and a history table, corrections are two writes rather than one, and
`AsOf` has to be threaded through query paths. It is carried in context rather
than in every method signature precisely because a `List`/`ListAsOf` pair would
eventually be used inconsistently.

`pkg/buildinfo` exists from the first Go commit for this reason: it reports
`unknown` rather than an empty string for an unstamped build, and marks results
from a modified working tree as not reproducible, so an unreproducible result
is visibly unreproducible rather than silently recorded as if it were fine.
