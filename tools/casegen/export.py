#!/usr/bin/env python3
"""Write a pandapower network out as a MATPOWER case file.

The scale cases this project needs are public but are not distributed as ``.m``
files anywhere convenient. pandapower ships them, so the shortest honest path
to a large network is to ask the oracle for one and write it in the format the
importer already reads.

Written from pandapower's internal ``_ppc``, which *is* MATPOWER format, for the
same reason the golden generator uses it: converting from the user-facing model
would mean reimplementing pandapower's transformer and shunt handling, and the
case would then describe that reimplementation rather than the network.

The one thing the ppc cannot express is a transformer's magnetising
conductance — MATPOWER's branch format has a column for magnetising susceptance
and none for conductance. Any such term is folded into the bus shunt here, the
same way the golden generator derives it, so the written case is electrically
what pandapower solved rather than approximately so.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

import numpy as np
import pandapower as pp
import pandapower.networks as networks


# Above this, a stated rating is pandapower's "no limit" sentinel rather than a
# limit. The largest transformers ever built are around 1500 MVA.
UNRATED_ABOVE_MVA = 1_000_000


def matrix(rows, columns: int) -> str:
    """Render a ppc matrix as MATPOWER text, one row per line."""
    out = []
    for row in rows:
        cells = "\t".join(f"{float(row[index]):.10g}" for index in range(columns))
        out.append(f"\t{cells};")
    return "\n".join(out)


def derive_bus_shunts(ppc, ybus, n_bus: int):
    """Recover the bus shunt pandapower's Ybus implies but the ppc omits.

    Assembling what the ppc branches account for and subtracting it from
    pandapower's own Ybus diagonal is complete by construction: whatever
    element produced the term, the remainder carries it.
    """
    accounted = np.zeros(n_bus, dtype=complex)

    for row in ppc["branch"]:
        if row[10] <= 0:
            continue
        f, t = int(row[0].real), int(row[1].real)
        y_series = 1.0 / complex(row[2].real, row[3].real)
        b_charge = complex(0, row[4].real) / 2
        tap = complex(row[8].real, 0) if row[8].real != 0 else complex(1, 0)
        if row[9].real != 0:
            tap = abs(tap) * np.exp(1j * np.deg2rad(row[9].real))
        accounted[f] += (y_series + b_charge) / (abs(tap) ** 2)
        accounted[t] += y_series + b_charge

    return [complex(ybus[i, i]) - accounted[i] for i in range(n_bus)]


def write(name: str, out: Path) -> int:
    build = getattr(networks, name, None)
    if build is None:
        print(f"casegen: pandapower has no network named {name!r}", file=sys.stderr)
        return 1

    net = build()
    pp.runpp(net, numba=False, init="flat", calculate_voltage_angles=True)

    ppc = net._ppc
    base_mva = float(ppc["baseMVA"])
    buses = ppc["bus"]
    n_bus = len(buses)

    ybus = ppc["internal"]["Ybus"].todense()
    shunts = derive_bus_shunts(ppc, ybus, n_bus)

    # The derived shunt replaces the ppc's stated one. It is the same quantity
    # measured a more complete way, and mixing the two would double-count.
    written = buses.copy()
    for index in range(n_bus):
        written[index, 4] = shunts[index].real * base_mva
        written[index, 5] = shunts[index].imag * base_mva

    # pandapower spells "no thermal limit" as max_i_ka = 99999, which reaches
    # the ppc as a rating in the tens of millions of MVA. Written verbatim
    # those become branches that are rated and can never be overloaded, which
    # is a different claim from unrated — and the model has a proper
    # representation for unrated, which the importer and the solver both
    # honour. The largest transformers ever built are around 1500 MVA, so
    # anything past a million is a sentinel rather than a limit.
    written_branch = ppc["branch"].real.copy()
    sentinel = written_branch[:, 5] > UNRATED_ABOVE_MVA
    written_branch[sentinel, 5] = 0.0

    print(
        f"  {int(sentinel.sum())} of {len(written_branch)} branches carry no "
        f"thermal limit and are written unrated",
        file=sys.stderr,
    )

    text = f"""function mpc = {name}
%% Written by blackstart tools/casegen/export.py from pandapower.
%% Not hand-authored: this is pandapower's own internal MATPOWER array, with
%% bus shunts derived from its Ybus so a transformer's magnetising conductance
%% is not lost. See the module docstring for why.
mpc.version = '2';
mpc.baseMVA = {base_mva:.10g};

mpc.bus = [
{matrix(written.real, 13)}
];

mpc.gen = [
{matrix(ppc["gen"].real, 10)}
];

mpc.branch = [
{matrix(written_branch, 11)}
];
"""

    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(text)

    print(f"{out}: {n_bus} buses, {len(ppc['branch'])} branches, {base_mva:g} MVA base")

    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("network", help="a pandapower.networks name, e.g. case2869pegase")
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()

    return write(args.network, args.out)


if __name__ == "__main__":
    raise SystemExit(main())
