# Architecture decision records

Short records of decisions that were not obvious and would otherwise be
re-litigated. Each states the context, the decision, and, most importantly 
what the decision costs.

An ADR is written when a choice constrains later work, when a reasonable
engineer would have chosen differently, or when the reasoning depends on a
fact about the substrate that is not visible from the code.

ADRs are immutable once merged. A decision that is later reversed gets a new
ADR that supersedes the old one; the old one stays, marked superseded, because
the reasoning that led to it is part of the record.

| # | Title | Status |
|---|---|---|
| [0001](0001-substrate.md) | Build on Forge, Vessel, Grove and Fabriq | Accepted |
| [0002](0002-single-go-module.md) | One Go module, extensions as packages | Accepted |
| [0003](0003-fabriq-sole-write-path.md) | Fabriq is the sole entity write path | Accepted |
| [0004](0004-advisory-only.md) | Advisory only, never actuating | Accepted |
| [0005](0005-solver-determinism.md) | Same-machine determinism, documented tolerances | Accepted |
| [0006](0006-provenance.md) | Every number traceable to data, model and commit | Accepted |
