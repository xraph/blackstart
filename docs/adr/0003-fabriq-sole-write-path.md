# 0003 — Fabriq is the sole entity write path

**Status:** Accepted

## Context

The project's architectural claim is that a single write produces consistent
relational, graph, spatial and search views of the same network — a domain
where all four are genuinely needed. Fabriq implements that with a
transactional outbox, a leader-elected relay, and rebuildable projections.

The alternative was to use Grove directly for Phases 0–2, which need almost
none of it, and adopt Fabriq later when weather and search arrive. That defers
complexity but risks a retrofit touching every store.

## Decision

All entity writes go through `fabriq.Exec`. Graph projection is on from day
one. Entities are declared as Fabriq `EntitySpec`s.

**Blackstart writes its own Forge extension around `fabriq.Open` rather than
using Fabriq's `forgeext`.** This is forced, not preferred:
`forgeext.Extension.Start` calls `fabriq.Open(ctx, e.reg, cfg)` with no
variadic options, so `WithLifecycleHook`, `WithUpcasters` and `WithAuthz` are
all unreachable through it. `WithCustomAppliers` is the only code-level seam it
exposes.

## Consequences

The in-transaction hook is the payoff, and it is why owning the `Open` call
matters. `command.LifecycleHook.OnChange(ctx, tx, change)` runs inside the
write transaction, after the row and its outbox event are staged and before
commit, with a raw-SQL `tx.Exec` available. Two things depend on it:

- **Audit becomes gapless by construction.** If the audit write fails, the
  domain write rolls back. Grove's hooks cannot offer this — they have neither
  the aggregate identity nor the version.
- **Geometry becomes atomic with its entity.** Fabriq's spatial plane is
  direct-write, outside the outbox, with nothing reconciling a half-failed
  write. `projection.CustomApplier` cannot fix this: `projection.Mutation` is a
  sealed eight-member union whose only sinks are FalkorDB and Elasticsearch, so
  a geometry write is not expressible as a projection at all.

Costs, all real:

- **Redis and FalkorDB are mandatory**, not optional. Without a worker draining
  it, `fabriq_outbox` grows without bound and no projection advances. FalkorDB
  is the only graph adapter, with no injection point for another. Local
  development therefore needs three services, which is what `make dev-up` is
  for.
- **The registry must be assembled centrally, before `Open`.** Forge's default
  lifecycle interleaves `Register` and `Start` per extension, and Fabriq
  validates its registry inside `Open` during `Start` — so an extension
  declaring its `EntitySpec` in its own `Register` would register too late.
  Specs are collected eagerly in `internal/entities`.
- **Every entity table carries `id`, `tenant_id` and `version`**, plus an RLS
  policy written by hand; Fabriq generates DDL only for dynamic entities. The
  application must connect as a non-superuser or RLS is silently a no-op, so a
  boot assertion checks that.
- **Single-tenant means one constant tenant id.** Fabriq has no default-tenant
  mode by design; missing tenant context is a hard failure.
- A `GraphNode` declared with no graph adapter configured is silently ignored,
  so a boot assertion checks that too.

Bitemporality interacts awkwardly and the resolution is recorded here because
it is not obvious. Fabriq's `OpUpdate` mutates in place and bumps `version`,
while a bitemporal correction needs a new row. Rather than making the aggregate
an append-only version table — which would project one graph node per version,
forfeiting the very claim this decision exists to prove — the current table
stays the aggregate and a `LifecycleHook` writes the prior image to
`<table>_history` in the same transaction.
