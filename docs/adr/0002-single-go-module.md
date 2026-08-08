# 0002 — One Go module, extensions as packages

**Status:** Accepted

## Context

Forge's own extensions are each a separate Go module with a `replace` directive
pointing at the repository root. Following that pattern would mean a `go.mod`
per Blackstart extension — around a dozen, growing.

The house style exists for a good reason that does not apply here: Forge's
extensions are independently published and independently versioned libraries.
Blackstart's are not, and will not be. They are parts of one application that
ship together.

A module boundary would enforce the "no extension imports another's internal
packages" rule at compile time, which is the one genuine argument for it.

## Decision

One module, `github.com/xraph/blackstart`, with extensions as packages under
`extensions/<name>/`. A `go.work` exists so that the Cargo and pnpm workspaces
sit alongside and future modules can join.

`tools/` is a second module, so that build-time tooling — buf, protoc plugins,
linters — never enters the dependency graph of anyone importing Blackstart as a
library.

## Consequences

No version lockstep problem: a dozen `go.mod` files each requiring a specific
version of the others is a real maintenance cost, and Forge's own extensions
demonstrate it — several are pinned five minor versions behind core and some
have not been tagged in months.

The cost is that the "no cross-extension internal imports" rule is enforced by
review and lint rather than by the compiler. This is a genuine loss. It is
accepted because the rule is also easy to check by eye — an import of
`extensions/x/...` from `extensions/y/...` is visible in the import block — and
because cross-extension access has a positive convention to follow: resolve the
published interface from Vessel.

The Cargo and pnpm workspace roots live in `rust/` and `web/` rather than at the
repository root, which already carries `go.mod` and `go.work`. Neither tool
cares where its root is, and the Makefile bridges all three.
