# Security policy

## Reporting a vulnerability

Please report security issues privately through
[GitHub Security Advisories](https://github.com/xraph/blackstart/security/advisories/new),
or by email to **rex.i.raphael@gmail.com**.

Please do not open a public issue for a security problem.

Include what you need to make the problem reproducible: affected version or commit,
configuration, and the steps or input that trigger it. If you have a proof of concept,
please attach it rather than publishing it.

You can expect an acknowledgement within 72 hours and an assessment within 10 working
days. This is a pre-release project maintained by a small team; if a fix will take
longer than that, you will be told so and given a timeline rather than silence.

## Scope

In scope: the Blackstart platform, the Rust solver, the parsers and data connectors,
the reference client, and the build and release tooling in this repository.

Of particular interest, because they are the surfaces most likely to matter:

- The XML parsers (IEC 61850 SCL, CIM/CGMES) and the GRIB2 decoder, all of which
  consume files that may be attacker-supplied.
- Any path that would let untrusted input escape the read-only boundary described in
  [docs/SAFETY.md](docs/SAFETY.md).
- Authentication, authorization, and tenant isolation in the platform.

Out of scope: the upstream dependencies themselves (report those upstream, though we
would like to know), and findings that require an already-compromised host.

## Critical infrastructure information

Blackstart is built and tested exclusively on public data and on synthetic networks.
**Do not send us Critical Energy/Electric Infrastructure Information (CEII), utility
network models, or operational data as part of a report.** If reproducing an issue
appears to require such data, say so and we will work out a synthetic reproduction
together.

Analysis results identifying vulnerabilities in a real, named utility network are not
published in this repository and will not be accepted in issues or pull requests. The
Texas A&M ACTIVSg synthetic cases exist for exactly this reason.

## Advisory-only boundary

A defect that would allow Blackstart to write to a SCADA, EMS, ADMS, or field device 
whether through a protocol adapter, an API, or a dependency, is treated as a critical
security vulnerability regardless of how difficult it is to exploit. The absence of
that capability is a safety property of the system, and any erosion of it is a bug of
the highest severity.

## Supported versions

None yet. The project is in early development and has no releases. Until the first
tagged release, security fixes land on `main`.
