#!/usr/bin/env python3
"""Check the committed goldens against a second, independent oracle.

The goldens are generated from pandapower. Comparing the solver to them proves
agreement with one tool, and one tool agreeing with itself is not evidence: a
shared misreading of the case — a transposed column, a base applied twice —
would satisfy every check and be wrong in both.

pypower is the second lineage. It is a direct translation of MATPOWER's
algorithms rather than a reimplementation, so where it agrees with pandapower
the agreement spans two independently maintained descriptions of the same
network. Where it disagrees, the disagreement is worth reading.

This deliberately reads the golden's own `case` section — blackstart's per-unit
representation — rather than rebuilding the network from pandapower. What is
under test is whether the case blackstart stored means the same thing to a
second solver, and rebuilding from the first oracle would test something else.

# Status: a diagnostic, not a gate

Three of the five goldens — 30, 57 and 118 — agree with pypower to machine
precision, around 1e-15. That is strong evidence the reconstruction below is
correct: a transposed column or a mishandled base could not produce
fifteen-digit agreement on three networks and fail on two.

Cases 14 and 300 disagree, by 1.6e-3 and 7.1e-2 per unit. That is **not
explained**, and this tool is deliberately not wired into CI until it is.
Two hypotheses have been ruled out already:

  - Reactive limits. Both solvers run unlimited here — the goldens are
    generated with enforce_q_lims=False and pypower's default is off — so the
    disagreement cannot be a generator pinned on one side and free on the
    other. (pypower's ENFORCE_Q_LIMS path additionally crashes on current
    numpy, so it cannot be tested from this side at all.)
  - The presence of stated limits in the case. All five carry them for every
    PV bus, so limits do not separate the passing cases from the failing ones.

A third has been ruled out since, and it narrows things usefully. Every
disagreeing bus in case 14 is downstream of an off-nominal tap — buses 3, 4, 6
and 8 through 13, while every voltage-fixed bus agrees exactly — so the
difference lives in transformer branch modelling. Inverting the stored tap
makes it worse (1.6e-3 becomes 2.8e-2), which rules out the alarming
possibility that blackstart stores the ratio the wrong way round.

That leaves a convention difference between the two oracles rather than a
defect here: blackstart's own solver already matches pandapower to 1e-14 on
these cases, so it is pypower that differs from pandapower, not blackstart
that differs from both.

But "transformer branch modelling" is as far as the evidence goes, and an
earlier draft of this comment over-claimed. Off-nominal tap *count* does not
predict which cases disagree: case 57 has fifteen and agrees to 4e-15, case
118 has nine and agrees to 1e-15, while case 14 has three and disagrees by
1.6e-3. Whatever the difference is, it is a property of particular
transformers rather than of having them — the pattern within case 14 (every
disagreeing bus downstream of a tap) is real, and the pattern across cases is
not yet explained.

Weakening the tolerance until it passes would defeat the point of having a
second oracle. Until the difference is understood, this reports and does not
gate.
"""

from __future__ import annotations

import argparse
import json
import math
import sys
from pathlib import Path

import numpy as np
from pypower.api import ppoption, runpf

# MATPOWER bus types.
PQ, PV, REF = 1, 2, 3

KIND = {"pq": PQ, "pv": PV, "slack": REF}


def to_ppc(case: dict) -> dict:
    """Build a MATPOWER array from the golden's per-unit case.

    The golden is already per unit on the case's own base, which is what
    MATPOWER wants — but MATPOWER states injections in MW and MVAr, so the
    powers multiply back by the base. Getting that wrong is a factor of 100
    that still converges.
    """
    base = case["base"]["s_base_mva"]
    buses, branches = case["buses"], case["branches"]

    bus = np.zeros((len(buses), 13))
    gen_rows = []

    for index, b in enumerate(buses):
        kind = KIND[b["kind"]]
        bus[index] = [
            index + 1,                      # bus_i, one-based
            kind,
            -b["p_injection"] * base if b["p_injection"] < 0 else 0.0,   # Pd
            -b["q_injection"] * base if b["q_injection"] < 0 else 0.0,   # Qd
            b["g_shunt"] * base,            # Gs
            b["b_shunt"] * base,            # Bs
            1,                              # area
            b["v_magnitude"],
            math.degrees(b["v_angle"]),
            1.0,                            # baseKV — per unit already
            1, 1.06, 0.94,
        ]

        # A generator wherever the bus holds voltage or supplies power. The
        # golden carries a net injection rather than a generator list, so the
        # positive part becomes generation and the negative part became load
        # above. Splitting it this way is what makes the two descriptions the
        # same network rather than merely similar ones.
        if kind in (PV, REF) or b["p_injection"] > 0:
            q_min = b["q_min"]
            q_max = b["q_max"]
            gen_rows.append([
                index + 1,
                max(b["p_injection"], 0.0) * base,
                max(b["q_injection"], 0.0) * base,
                (q_max * base) if q_max is not None else 1e6,
                (q_min * base) if q_min is not None else -1e6,
                b["v_magnitude"],
                base, 1, 1e4, -1e4,
            ])

    branch = np.zeros((len(branches), 13))
    for index, br in enumerate(branches):
        branch[index] = [
            br["from"] + 1, br["to"] + 1,
            br["r"], br["x"], br["b"],
            br["rating"], 0, 0,
            br["tap"], math.degrees(br["shift"]),
            1 if br["in_service"] else 0,
            -360, 360,
        ]

    return {
        "version": "2",
        "baseMVA": base,
        "bus": bus,
        "gen": np.array(gen_rows),
        "branch": branch,
    }


def compare(path: Path, tol_v: float, tol_a: float) -> tuple[bool, str]:
    golden = json.loads(path.read_text())
    reference = golden["reference"]

    result, converged = runpf(
        to_ppc(golden["case"]),
        ppoption(VERBOSE=0, OUT_ALL=0, PF_ALG=1, PF_TOL=1e-10, PF_MAX_IT=50),
    )

    if not converged:
        return False, "pypower did not converge"

    got_v = result["bus"][:, 7]
    got_a = np.radians(result["bus"][:, 8])
    want_v = np.array(reference["v_magnitude"])
    want_a = np.array(reference["v_angle"])

    # Angles are compared as differences from the reference bus. Both solvers
    # are free to choose an absolute datum and a constant offset across every
    # bus is the same network, not a disagreement.
    got_a = got_a - got_a[0]
    want_a = want_a - want_a[0]

    dv = float(np.max(np.abs(got_v - want_v)))
    da = float(np.max(np.abs(got_a - want_a)))

    ok = dv <= tol_v and da <= tol_a
    worst = int(np.argmax(np.abs(got_v - want_v)))

    return ok, (
        f"max |Δ|V|| = {dv:.3e} pu (bus {worst}), "
        f"max |Δθ| = {da:.3e} rad"
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dir", type=Path, default=Path("data/golden"))
    # Loose next to the solver's own tolerances on purpose. pypower and
    # pandapower are two independent codebases with different convergence
    # criteria and different handling of reactive limits; the question here is
    # whether they describe the same network, not whether they agree to machine
    # precision. A bound tight enough to fail on the last bits would fail for
    # reasons that say nothing about blackstart.
    parser.add_argument("--tol-v", type=float, default=1e-3)
    parser.add_argument("--tol-a", type=float, default=1e-3)
    args = parser.parse_args()

    goldens = sorted(args.dir.glob("*.golden.json"))
    if not goldens:
        print(f"secondoracle: no goldens in {args.dir}", file=sys.stderr)
        return 1

    failures = 0

    for path in goldens:
        ok, detail = compare(path, args.tol_v, args.tol_a)
        print(f"{'ok  ' if ok else 'FAIL'} {path.name}: {detail}")
        if not ok:
            failures += 1

    print(f"\n{len(goldens) - failures}/{len(goldens)} agree with pypower")

    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
