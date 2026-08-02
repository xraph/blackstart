# Golden power-flow cases

Generated, not hand-written. Regenerate with:

```
python3 -m venv .venv && .venv/bin/pip install pandapower
.venv/bin/python tools/goldengen/generate.py --out data/golden
```

Never edit these by hand. Each file carries a `provenance` block naming the
generator, the commit it ran at, the oracle and its version; editing the
numbers without that header changing makes the header a lie, and the header is
the only reason a golden is evidence rather than a number someone wrote down.

## Contents

| File | Network | Source | Licence |
|---|---|---|---|
| `case14.newton.golden.json` | IEEE 14-bus | pandapower `networks.case14` | BSD-3 (pandapower); the network data is IEEE test material published for benchmarking |
| `case30.newton.golden.json` | IEEE 30-bus | pandapower `networks.case30` | as above |
| `case57.newton.golden.json` | IEEE 57-bus | pandapower `networks.case57` | as above |
| `case118.newton.golden.json` | IEEE 118-bus | pandapower `networks.case118` | as above |
| `case300.newton.golden.json` | IEEE 300-bus | pandapower `networks.case300` | as above |

These are the standard IEEE test networks, published for exactly this purpose.
They are synthetic and describe no real utility's system, which is why solutions
to them can be committed to a public repository.

## What the reference is

An unlimited Newton-Raphson solve: `enforce_q_lims=False`, recorded in each
file's provenance. Blackstart's comparison must therefore use its *unlimited*
solve. A limited and an unlimited solve answer different questions, and
comparing one against the other produces a disagreement that looks like a bug
and is not.

Validating reactive-limit enforcement needs a second set generated with
`enforce_q_lims=True`, which is not yet here.

## A gap in the ppc worth knowing about

The generator reads pandapower's internal MATPOWER-format `ppc`, which is
almost but not quite a complete description. MATPOWER's branch format has a
column for magnetising susceptance and none for magnetising conductance, so a
transformer with non-zero `i0_percent` contributes a diagonal term to
pandapower's `Ybus` that appears nowhere in the `ppc`.

IEEE 14, 30 and 57 have no such transformer and agreed to machine precision on
the first attempt. IEEE 118 and 300 do, and disagreed by 2e-4 — localised to
six diagonal entries, real part only, at exactly the terminals of the four
transformers with magnetising current.

Bus shunts are therefore *derived*: assemble what the branches account for,
subtract that from pandapower's own `Ybus` diagonal, and record the remainder.
Complete by construction for any element pandapower can represent, including
ones the generator has never heard of.
