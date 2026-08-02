#!/usr/bin/env python3
"""Generate golden power-flow cases and reference solutions from pandapower.

Each golden file carries three things: the network in blackstart's own
per-unit representation, pandapower's solution to it, and a provenance header
saying exactly what produced both. A number without that header is not
reproducible, and this project's whole claim rests on its numbers being
reproducible.

The bridge is pandapower's internal ``_ppc`` array, which is MATPOWER format
and already per-unit. Converting from pandapower's user-facing model instead
would mean reimplementing its transformer and shunt handling, and the golden
would then be testing that reimplementation rather than the solver.

The ppc is not quite complete, though, and the gap is worth stating because it
cost real time to find. MATPOWER's branch format has a column for magnetising
*susceptance* and none for magnetising *conductance*, so a transformer with a
non-zero ``i0_percent`` contributes a diagonal term to pandapower's Ybus that
appears nowhere in the ppc. IEEE 14, 30 and 57 have no such transformer and
agreed to machine precision; 118 and 300 do, and disagreed by 2e-4.

Rather than enumerate pandapower's element types and hope the list is complete,
the bus shunt is *derived*: assemble what the ppc branches account for, subtract
it from pandapower's own Ybus diagonal, and record the remainder. That is
complete by construction for any element pandapower can represent, including
ones this script has never heard of.

Usage:

    python3 tools/goldengen/generate.py --out data/golden

pandapower is BSD-3-licensed and pip-installable. It is a development
dependency of this repository, never a runtime one — nothing in the shipped
Go or Rust depends on it.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path

try:
    import numpy as np
    import pandapower as pp
    import pandapower.networks as nw
except ImportError:  # pragma: no cover - a setup problem, not a code path
    sys.exit(
        "pandapower and numpy are required.\n"
        "  python3 -m venv .venv && .venv/bin/pip install pandapower\n"
        "  .venv/bin/python tools/goldengen/generate.py"
    )

# MATPOWER column indices. Spelled out rather than sliced by number at the use
# site, because a silently shifted column produces a plausible network.
BUS_I, BUS_TYPE, PD, QD, GS, BS = 0, 1, 2, 3, 4, 5
BUS_VM, BUS_VA = 7, 8
BR_F, BR_T, BR_R, BR_X, BR_B, BR_RATE_A = 0, 1, 2, 3, 4, 5
BR_TAP, BR_SHIFT, BR_STATUS = 8, 9, 10
GEN_BUS, GEN_PG, GEN_QG, GEN_QMAX, GEN_QMIN, GEN_VG = 0, 1, 2, 3, 4, 5
GEN_STATUS = 7

# MATPOWER bus types.
PQ, PV, REF = 1, 2, 3

# The networks to generate. Each is a standard IEEE test case, published for
# exactly this purpose, so there is no licensing question about committing a
# solution to one.
CASES = [
    ("case14", nw.case14),
    ("case30", nw.case30),
    ("case57", nw.case57),
    ("case118", nw.case118),
    ("case300", nw.case300),
]


def commit() -> str:
    """The commit this golden was generated at, or a marker saying it is dirty.

    A golden generated from uncommitted code cannot be traced to anything, so
    it says so rather than recording a commit that does not describe it.
    """
    try:
        sha = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], text=True, stderr=subprocess.DEVNULL
        ).strip()
        dirty = subprocess.check_output(
            ["git", "status", "--porcelain"], text=True, stderr=subprocess.DEVNULL
        ).strip()
        return f"{sha}-dirty" if dirty else sha
    except (subprocess.CalledProcessError, FileNotFoundError):
        return "unknown"


def finite_or_null(value: float) -> float | None:
    """Maps an infinite limit onto JSON null, which means unlimited."""
    return float(value) if np.isfinite(value) else None


def derive_bus_shunts(ppc: dict, ybus, branches: list[dict], n_bus: int) -> list[complex]:
    """The diagonal admittance pandapower has that the branches do not explain.

    Assembles the branch contribution to each diagonal exactly as blackstart
    will, subtracts it from pandapower's own Ybus, and returns the remainder as
    each bus's shunt. Anything pandapower models as a shunt — capacitor banks,
    transformer magnetising branches, wards, impedances — lands here without
    this script needing to know it exists.
    """
    from_branches = np.zeros(n_bus, dtype=complex)

    for br in branches:
        if not br["in_service"]:
            continue
        y = 1.0 / complex(br["r"], br["x"])
        charging = 1j * br["b"] / 2.0
        tap = br["tap"]
        from_branches[br["from"]] += (y + charging) / (tap * tap)
        from_branches[br["to"]] += y + charging

    diagonal = np.asarray(ybus.todense()).diagonal()

    return [complex(diagonal[i] - from_branches[i]) for i in range(n_bus)]


def to_case(ppc: dict, name: str) -> dict:
    """Converts a MATPOWER-format ppc into blackstart's case representation."""
    base_mva = float(ppc["baseMVA"])
    bus_rows = ppc["bus"]
    gen_rows = ppc["gen"]

    # MATPOWER bus numbers are not necessarily 0..n-1, and blackstart's indices
    # are. The map is built once here so every branch reference goes through it.
    index_of = {int(row[BUS_I]): i for i, row in enumerate(bus_rows)}

    # Generator output accumulates onto its bus: several machines can sit on one.
    gen_p = [0.0] * len(bus_rows)
    gen_q_min = [float("-inf")] * len(bus_rows)
    gen_q_max = [float("inf")] * len(bus_rows)
    scheduled_v = {}

    for row in gen_rows:
        if int(row[GEN_STATUS]) <= 0:
            continue
        bus = index_of[int(row[GEN_BUS])]
        gen_p[bus] += float(row[GEN_PG]) / base_mva

        # Limits add across machines at a bus. A machine with no limit makes
        # the whole bus unlimited, which is the honest reading.
        lo, hi = float(row[GEN_QMIN]), float(row[GEN_QMAX])
        if np.isfinite(lo) and np.isfinite(hi):
            base_lo = 0.0 if gen_q_min[bus] == float("-inf") else gen_q_min[bus]
            base_hi = 0.0 if gen_q_max[bus] == float("inf") else gen_q_max[bus]
            gen_q_min[bus] = base_lo + lo / base_mva
            gen_q_max[bus] = base_hi + hi / base_mva

        vg = float(row[GEN_VG])
        if np.isfinite(vg):
            scheduled_v[bus] = vg

    buses = []
    for i, row in enumerate(bus_rows):
        kind = {PQ: "pq", PV: "pv", REF: "slack"}[int(row[BUS_TYPE])]

        # Net injection is generation less load. Shunts are not folded in here
        # — they are admittance, not injection, and depend on the voltage.
        p = gen_p[i] - float(row[PD]) / base_mva
        q = -float(row[QD]) / base_mva

        buses.append(
            {
                "id": f"bus-{int(row[BUS_I])}",
                "kind": kind,
                # The slack's injection is whatever the network needs, so it is
                # left at zero rather than seeded with pandapower's answer.
                "p_injection": 0.0 if kind == "slack" else p,
                "q_injection": q,
                "v_magnitude": scheduled_v.get(i, float(row[BUS_VM]))
                if kind in ("pv", "slack")
                else 1.0,
                # The reference angle is a property of the case: IEEE 118 puts
                # it at 30°, not 0. Recording only the slack's leaves every
                # other bus at a genuinely flat start.
                "v_angle": float(np.deg2rad(row[BUS_VA])) if kind == "slack" else 0.0,
                # JSON has no infinity, so null means unlimited. That is the
                # more honest encoding anyway: a generator with no stated
                # limits has unknown limits, and a magic large number would be
                # indistinguishable from a real one.
                "q_min": finite_or_null(gen_q_min[i]) if kind == "pv" else None,
                "q_max": finite_or_null(gen_q_max[i]) if kind == "pv" else None,
                # Filled in below, once the branches are known.
                "g_shunt": 0.0,
                "b_shunt": 0.0,
            }
        )

    branches = []
    for j, row in enumerate(ppc["branch"]):
        tap = float(row[BR_TAP])
        branches.append(
            {
                "id": f"branch-{j}",
                "from": index_of[int(row[BR_F])],
                "to": index_of[int(row[BR_T])],
                "r": float(row[BR_R]),
                "x": float(row[BR_X]),
                "b": float(row[BR_B]),
                "rating": float(row[BR_RATE_A]) / base_mva,
                "in_service": int(row[BR_STATUS]) > 0,
                # MATPOWER writes 0 for "no transformer"; 1.0 is the identity.
                "tap": tap if tap > 0 else 1.0,
                "shift": float(np.deg2rad(row[BR_SHIFT])),
            }
        )

    shunts = derive_bus_shunts(
        ppc, ppc["internal"]["Ybus"], branches, len(bus_rows)
    )
    for bus, shunt in zip(buses, shunts):
        bus["g_shunt"] = shunt.real
        bus["b_shunt"] = shunt.imag

    return {
        "name": name,
        "base": {"s_base_mva": base_mva, "v_base_kv": 138.0},
        "buses": buses,
        "branches": branches,
    }


def to_solution(ppc: dict) -> dict:
    """pandapower's answer, in the same indexing as the case."""
    return {
        "v_magnitude": [float(row[BUS_VM]) for row in ppc["bus"]],
        "v_angle": [float(np.deg2rad(row[BUS_VA])) for row in ppc["bus"]],
    }


def generate(name: str, build, out_dir: Path, generator_commit: str) -> Path:
    net = build()

    # Flat start and no numba: the golden must not depend on a JIT being
    # available, and a flat start is what blackstart uses, so a disagreement
    # is about the equations rather than about where each solver began.
    # enforce_q_lims is passed explicitly rather than left to the default,
    # because which problem the oracle solved is part of what the golden means.
    # False here: the reference is an unlimited solve, and blackstart's
    # comparison must use its unlimited solve too or the two are answering
    # different questions.
    pp.runpp(
        net,
        algorithm="nr",
        init="flat",
        numba=False,
        tolerance_mva=1e-10,
        enforce_q_lims=False,
    )
    if not net["converged"]:
        raise RuntimeError(f"{name}: pandapower did not converge")

    ppc = net._ppc
    case = to_case(ppc, name)

    document = {
        "provenance": {
            "generator": "tools/goldengen/generate.py",
            "generator_commit": generator_commit,
            "oracle": "pandapower",
            "oracle_version": pp.__version__,
            "method": "newton-raphson",
            "enforce_q_lims": False,
            "case": name,
            "note": (
                "Generated from pandapower's internal MATPOWER-format ppc, "
                "which is already per-unit. Regenerate with "
                "tools/goldengen/generate.py; never hand-edit."
            ),
        },
        "case": case,
        "reference": to_solution(ppc),
    }

    # Sorted keys and a fixed separator so the file is byte-stable across runs
    # and a regeneration that changes nothing produces no diff.
    body = json.dumps(document, indent=2, sort_keys=True, allow_nan=False)

    path = out_dir / f"{name}.newton.golden.json"
    path.write_text(body + "\n")
    return path


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, default=Path("data/golden"))
    args = parser.parse_args()

    args.out.mkdir(parents=True, exist_ok=True)
    generator_commit = commit()

    if generator_commit.endswith("-dirty"):
        print(
            "warning: the working tree is dirty; these goldens cannot be "
            "traced to a commit and are labelled accordingly",
            file=sys.stderr,
        )

    for name, build in CASES:
        path = generate(name, build, args.out, generator_commit)
        digest = hashlib.sha256(path.read_bytes()).hexdigest()[:16]
        print(f"{path}  sha256:{digest}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
