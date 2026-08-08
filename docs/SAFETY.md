# Safety boundary

This document is normative. It describes a constraint on the software, not a
description of its current maturity. Nothing in a later phase relaxes it.

## Blackstart is advisory and does not actuate

Blackstart produces forecasts, rankings, and recommendations for human review. It
does not issue control commands and has no mechanism to do so.

Concretely, and enforced rather than promised:

1. **No write path to operational systems.** No code in this repository writes to a
   SCADA, EMS, ADMS, RTU, relay, or any other field device.
2. **Protocol adapters are read-only clients.** The IEC 61850 (MMS and GOOSE), DNP3,
   and IEC 60870-5-104 adapters implement reads, polls, and subscriptions only. They
   expose no method that writes a control point, issues a select-before-operate, sets
   a tap position, or changes a setpoint. Blackstart's IEC 61850 support parses
   configuration and subscribes to reports; it does not originate control blocks.
3. **No control API.** No REST, gRPC, CLI, or streaming endpoint accepts an
   instruction destined for a physical device.
4. **No forward-looking placeholders.** There is no `TODO`, stub, feature flag, or
   commented-out branch anticipating an actuation capability. A reader auditing this
   repository should find no evidence that control is planned, because it is not.

## Why this is architectural

A system that ranks contingencies by probability is, by construction, a system that
knows which switching actions would help. The distance between "recommend" and
"execute" is therefore small in code and very large in consequence. Drawing the line
at the repository boundary — rather than at a configuration flag or a deployment
choice — is the only version of this commitment that a reviewer can verify.

It also keeps the safety case simple. Blackstart cannot cause an outage it predicted,
cannot be induced to act by a corrupted forecast, and cannot be repurposed by an
attacker who compromises the platform, because the capability is absent rather than
disabled.

## How it is enforced

- A lint rule rejects identifiers matching `(?i)(write|send|issue|execute)(Control|Command|Setpoint|Breaker)`
  in protocol and adapter packages. It runs in the required CI check.
- Protocol client interfaces are defined with read methods only; adding a write method
  is a visible interface change, not an implementation detail.
- Architecture decision record `docs/adr/0004-advisory-only.md` records the decision
  and the reasoning.

A promise that continuous integration cannot check is not an architectural boundary.
If you find a gap in this enforcement, it is a bug — please report it per
[SECURITY.md](../SECURITY.md).

## Operational recommendations

Where Blackstart produces recommended actions — crew pre-positioning, candidate
switching plans, load transfer options, dynamic rating headroom — those are presented
for a qualified human to evaluate and, if they choose, carry out through their own
operational systems. Recommendations are labelled as such in the API and the client,
and carry the provenance reference needed to understand where the number came from.

## Published analysis

Vulnerability analysis, ranked contingency lists, and cascade results are published in
this repository only for synthetic test systems — principally the Texas A&M ACTIVSg
cases, which are statistically realistic but correspond to no real utility's network
and are free of Critical Energy/Electric Infrastructure Information restrictions.

Results for a real, named utility network are not published here, and contributions
containing them will not be accepted.
