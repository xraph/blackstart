# 0004 — Advisory only, never actuating

**Status:** Accepted

## Context

Blackstart ranks contingencies by probability and simulates their consequences.
A system that can say "this switching action reduces expected unserved energy
by 40 MWh" is, structurally, a system that knows what to do. The distance from
there to doing it is a few hundred lines of protocol client.

The protocols involved — IEC 61850 MMS, DNP3, IEC 60870-5-104 — are
bidirectional by design. A library that reads DNP3 analog inputs almost always
also writes control relay output blocks; using one means the capability is
present whether or not it is called.

Meanwhile the platform consumes forecasts and telemetry from outside itself. If
actuation existed, a corrupted forecast would become an attack path to the
grid, and compromising the platform would mean compromising the plant.

## Decision

Blackstart contains no actuation capability. Not disabled, not gated, not
behind a flag — absent.

Concretely: no write path to any SCADA, EMS, ADMS, RTU or relay; protocol
adapters expose reads, polls and subscriptions only; no control API on any
transport; and no `TODO`, stub or commented-out branch anticipating one.

The boundary is drawn at the repository, not at a configuration option or a
deployment choice, because that is the only version a reviewer can verify by
reading the code.

## Consequences

The safety case becomes simple enough to state in a sentence: Blackstart cannot
cause the outage it predicted, cannot be induced to act by a corrupted input,
and cannot be repurposed by an attacker who compromises it — because the
capability is not there to find.

The cost is real. Recommendations must be carried out by a human through their
own systems, which is slower and loses the closed loop that would make some
products more valuable. Integrators wanting automated response must build that
themselves, outside this repository, and take on the safety case for it.

Enforcement is mechanical, in two layers, because a promise CI cannot check is
not an architectural boundary:

- The `forbidigo` block in `.golangci.yml` rejects call sites, including calls
  into third-party libraries that text search cannot resolve.
- `scripts/check-safety-boundary.sh` rejects declarations, which forbidigo
  cannot see — an interface method is invisible to it until something calls it.

Both were verified against probe files rather than assumed to work.

See [docs/SAFETY.md](../SAFETY.md) for the normative statement.
