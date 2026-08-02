# Cases

Every network here is public and synthetic. Nothing in this directory
describes a real utility's system, and nothing in this repository publishes
vulnerability analysis for one.

## case2869pegase.m

| | |
|---|---|
| Origin | PEGASE, via `pandapower.networks.case2869pegase` |
| Size | 2869 buses, 4582 branches, 100 MVA base |
| Licence | BSD-3-Clause, as distributed with pandapower |
| Written by | `tools/casegen/export.py`, from pandapower's internal `_ppc` |

Not hand-authored and not downloaded. pandapower ships the case, so the
shortest honest path to a network at scale is to ask the oracle we already
depend on for one and write it in the format the importer reads. Anyone with
pandapower installed can regenerate this file and get the same bytes; there is
no download step and no licence to negotiate.

Two things about it are worth knowing before reading any number computed from
it.

**It has base-case violations.** At least one transformer sits above its
rating before any outage — pandapower's own `res_trafo.loading_percent`
reports a maximum of 895%, and this project's AC solve agrees within the
difference between real and apparent power. A branch already over its rating
appears in every contingency, so an N-1 sweep of this network reports
thousands of findings that the outages did not cause. `bsctl n1` says so
explicitly rather than letting the ranking be swamped.

**A third of its branches have no thermal limit.** pandapower spells that as
`max_i_ka = 99999`, which reaches the MATPOWER array as a rating in the tens
of millions of MVA. Written verbatim those would be branches that are rated
and can never be overloaded, which is a different claim from unrated — so the
exporter writes them as unrated, which the importer and the solver both
already handle correctly. 1584 of 4582 branches are affected.
