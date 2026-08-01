# Contributing to Blackstart

Thank you for looking. This document covers the conventions that are enforced, so you
can spend your effort on the work rather than on rework.

Blackstart sits at the intersection of power systems engineering, meteorology, and
distributed systems. Contributors will be stronger in some of those than others, and
that is expected. Code here is written to be read by domain researchers who are not Go
or Rust specialists: prefer the boring, obvious implementation over the clever one.

## Before you start

For anything beyond a typo fix, please open an issue first. The project follows a
phased plan, and a well-built contribution to a phase that has not started yet is
still a contribution that cannot be merged.

## Commit conventions

**The commit history is an artifact of this project, not a side effect of producing
one.** It is meant to be readable as a record of how the system was built.

- **[Conventional Commits](https://www.conventionalcommits.org/)**:
  `feat(topology): …`, `fix(powerflow): …`, `test(scl): …`, `docs: …`, `chore: …`.
  The scope is an extension name, a crate name, or one of the shared scopes
  (`repo`, `ci`, `deps`, `quantity`, `bitemporal`, `hooks`, `id`, `store`, `proto`,
  `web`, `bsctl`, `solver`).
- **No commit adds more than roughly 500 lines of hand-written code.** If a unit of
  work is larger, decompose it. Generated code, vendored dependencies, and data
  fixtures are excluded — the exclusion list is `.gitattributes`, and the check reads
  it directly so the two cannot drift apart.
- **Tests land in the same commit as the code they cover.** Not in a follow-up.
- **History is never rewritten.** No squashing, no force-pushing to `main`, no
  interactive rebase of merged work. Pull requests are merged with merge commits;
  squash-merge and rebase-merge are disabled on the repository for this reason.

The commit body matters more than usual here. Explain why, not what — the diff already
says what.

`make setup` installs a `commit-msg` hook that checks these locally, so you find out
before you push rather than in CI.

## Building an extension

Extensions are the unit of work, and every one is built in the same seven stages, in
this order. Do not start a new extension until the current one passes all seven.

1. **Foundations** — TypeID prefixes (declared once, immutable thereafter), public
   interfaces, ORM models, DI key, database migration
2. **Stores** — store interface, then the PostgreSQL implementation
3. **Services** — managers and domain services holding the business logic, with no
   transport concerns
4. **Transport** — HTTP controllers, gRPC handlers, request and response types
5. **Registration** — extension registration, DI wiring, host application wiring
6. **Hooks** — hook integration and lifecycle events, with the acting identity carried
   through
7. **Tests** — unit tests with mocked stores, integration tests against a real
   database, golden-file tests for parsers and solvers

Rules that apply throughout:

- No extension imports another extension's internal packages. Cross-extension access
  goes through published interfaces resolved from the container.
- Electrical quantities are typed `Quantity` values carrying magnitude, unit, and
  per-unit-versus-SI reference base — **never bare `float64`**. Mixing per-unit and
  physical units is the classic power-systems bug class.
- Every fact carries valid time (when it was true of the world) and knowledge time
  (when the system learned it). This is not optional and not deferrable.
- Never construct database drivers directly, and never unwrap the Postgres driver —
  `pgdriver.Unwrap` panics on any other driver and is banned by lint.

## Numerical code

Anything that produces a number that a user might act on carries a higher bar.

- **No power-systems mathematics in Go except unit conversion.** If a Go file computes
  an admittance, it belongs in the Rust solver.
- Numerical code is validated against an established reference implementation
  (pandapower, MATPOWER) with committed golden files, and the tolerances are chosen by
  measurement and documented with a rationale. **If results diverge, fix the solver —
  do not loosen the tolerance.**
- The solver is deterministic: fixed iteration order, no unordered map traversal
  affecting results. Bit-identical results across architectures are not achievable and
  are not promised; same-machine determinism is, and is tested.

## Data

Every dataset must be publicly downloadable without an NDA, a license negotiation, or
a utility relationship. The allowed list is in the README. **Adding a source outside it
requires discussion first** — please open an issue rather than a pull request.

Every file under `data/` carries a row in the adjacent `SOURCES.md` giving its source
URL, content hash, retrieval date, and license basis. A file lands only when that row
is filled in.

## Licensing

Blackstart is Apache-2.0. Contributions are accepted under the same terms.

**GPL-family code cannot be linked or vendored.** This matters concretely: `libiec61850`
is GPLv3, so the IEC 61850 SCL parser is written from the published IEC 61850-6 schema
rather than wrapping it. GPL tools may be used as external test oracles run out of
process, never linked. A CI job enforces the boundary; please do not work around it.

## Safety

Read [docs/SAFETY.md](docs/SAFETY.md) before contributing to anything that touches a
protocol adapter. Blackstart does not actuate, and pull requests adding a write path to
an operational system will be declined regardless of how it is guarded.

## Code of conduct

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).
