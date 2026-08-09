# 0005: Same-machine determinism, documented tolerances across machines

**Status:** Accepted

## Context

The solver's results are validated against pandapower and MATPOWER using
committed golden files. That only works if running the same case twice gives
the same answer.

Floating-point addition is not associative. Anything that changes the order of
a reduction changes the result in the last bits:

- Iterating a `HashMap` or `HashSet` yields a different order per process,
  because the hasher is randomly seeded.
- `rayon` reductions combine partial results in whatever order threads finish.
- Multiple codegen units may reorder operations.
- `-C target-cpu=native` enables FMA contraction, replacing `a * b + c`'s two
  roundings with one. The result is *more accurate* and *different*.

Bit-identical results across architectures would require pinning the
instruction set, forbidding FMA everywhere, and giving up meaningful
vectorisation. That is a large permanent cost for a property nobody needs.

## Decision

Two different promises, stated separately because conflating them is how this
goes wrong:

**Same machine, same build: bit-identical.** Tested today by solving the same
case twice in one process and asserting a byte-identical event sequence, load
loss and outcome (`the_event_sequence_is_reproducible`, `bs-cascade`). That
catches a `HashMap` on a numeric path and a collection-order tie-break, which
is what it is for.

It does not catch a cross-process difference, because a repeated run in one
process shares the hasher seed and the thread pool. The stronger test, many
runs under a shuffled pool comparing result hashes, is **not written yet**, and
until it is, the bit-identical promise is enforced by the rules below rather
than demonstrated end to end.

**Across machines: agreement within documented tolerances.** Golden-file
comparison uses per-quantity tolerances chosen by measurement, committed
alongside a written rationale for each number.

Supporting rules, enforced rather than documented:

- `clippy.toml` bans `std::collections::HashMap` and `HashSet` outright. Use
  `BTreeMap`, `BTreeSet`, or a sorted `Vec`.
- All entity ordering derives from the canonical blob's array order, which Go
  has already sorted by TypeID. Internal indices are array indices.
- `rayon` parallelises only over indexed collections, writing into a pre-sized
  `Vec` by index. Floating-point reductions across threads are performed as a
  *sequential* fold over the ordered result vector.
- `codegen-units = 1` in the release profile.
- `-C target-cpu=native` is never set for a build that produces or checks
  golden files.
- Iteration count is part of the recorded result, so goldens capture
  convergence behaviour and not merely the final answer.

## Consequences

Some performance is left on the table: a sequential final fold instead of a
parallel reduce, one codegen unit instead of sixteen, no `target-cpu=native`.
The cost is small relative to the factorization itself, and validation is worth
more than the margin.

The tolerance numbers must be justified rather than tuned. If a golden
comparison fails, the fix is the solver, never a wider tolerance. That rule is
only credible because the tolerances are committed with reasoning attached, so
loosening one is a visible change to a reviewed artifact.

Toolchain bumps travel in their own commit alongside any golden updates they
force, since a compiler change can legitimately move the last bits.
