# 0001 — Build on Forge, Vessel, Grove and Fabriq

**Status:** Accepted

## Context

Blackstart needs extension composition, dependency injection, an ORM with
migrations, and a data layer that can maintain relational, graph, spatial and
search views of the same network. Building those is a year of work that is not
the point of the project.

The XRAPH ecosystem provides all four as Apache-2.0 or MIT packages: Forge
(extension host), Vessel (DI), Grove (ORM, drivers, migrations), Fabriq (data
fabric). They are also the only permitted substrate — this repository is built
fresh on public packages, with no code, schemas or configuration derived from
any employer system.

## Decision

Build on Forge 1.9.x, Vessel 1.0.x, Grove 1.5.x and Fabriq 0.0.x. Go floor
1.26.0, which Forge and DTL both require.

## Consequences

Substantial machinery arrives free: extension lifecycle with topological
dependency ordering, typed DI, Go-code migrations with cross-group dependency
resolution, a transactional outbox with rebuildable graph and search
projections, and multi-tenancy with row-level security.

Four things the project brief assumed exist do **not**, and are therefore ours
to build. This was verified in source rather than inferred:

- **No `HookDispatcher`, no `ActorID`, no typed principal.** Forge carries auth
  identity as string keys (`ctx.Set("user", …)`) through interceptors. All
  knowledge of that convention is quarantined in one file, `pkg/hooks/forgebridge.go`,
  so a future typed principal is a one-function change.
- **No `StoreFactory` type.** It is the composite-store convention, not an API.
- **No capability interfaces** (`Queryable`, `Spatial`, `Temporal`, …). We
  define our own, and only ones with real implementers.
- **No bitemporal support anywhere.** See [0006](0006-provenance.md).

Two documented behaviours are wrong and cost time if trusted: Grove's migration
docs show an `Executor.NewCreateTable` that does not exist (the executor is
SQL-string only), and Grove's README recommends `pgdriver.Unwrap`, which panics
on any non-Postgres driver. The latter is banned by lint, with the checked type
assertion living in exactly one place.

Fabriq is version 0.0.6 with an explicitly unfrozen API, despite being roughly
70k lines with a 0.84 test ratio. Every Fabriq call routes through
`pkg/store/fabriqstore` so that a breaking change is a one-package fix.

Version pinning is exact, with no `replace` directives.
