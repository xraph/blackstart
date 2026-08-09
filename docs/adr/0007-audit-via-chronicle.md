# 7. Audit through Chronicle, not a hand-written extension

Date: 2026-08-02

## Status

Accepted. Supersedes the `audit` extension described in the build plan.

## Context

The plan specified a hand-written `audit` extension with two paths: mutations
through a `command.LifecycleHook` running inside the write transaction, and
domain events through `pkg/hooks.Bus` out of transaction. `AuditEvent` was to be
append-only, enforced by a `BEFORE UPDATE OR DELETE` trigger plus a `REVOKE`.

That design is sound and it is also a solved problem. `github.com/xraph/chronicle`
is an existing Apache-2.0 extension in the same ecosystem, already a Forge
extension with a DI-injected emitter, and it does strictly more than the planned
extension would have:

- **Hash-chain integrity.** Every event links to the previous by SHA-256, so
  tampering breaks the chain and is detectable. The planned trigger-plus-REVOKE
  approach prevents modification by a well-behaved client and detects nothing;
  anyone with the right database grant could rewrite history silently.
- **GDPR crypto-erasure.** Per-subject AES-256-GCM encryption, so deleting a key
  makes one subject's data irrecoverable while the hash chain stays intact.
  Append-only storage and a right to erasure are otherwise in direct conflict,
  and the planned design had no answer.
- **Compliance reports.** SOC2 Type II, HIPAA and EU AI Act exports. This
  project publishes vulnerability analysis of power systems; being able to
  produce an access report without writing one is worth having.
- **Multi-tenant scoping, retention policies, and a 21-endpoint admin API.**

None of that was going to be written here, and a hand-rolled audit log that
merely appends is the kind of thing that looks finished until someone asks
whether it can be tampered with.

## Decision

Use `chronicle` as the audit trail. Do not write an `audit` extension.

Blackstart's part is to emit: the `pkg/hooks.Bus` subscriber that turns domain
events into chronicle records, and the entity metadata that decides what a
record says. The storage, the chain, the erasure and the reports are
chronicle's.

The same reasoning applies to two other planned pieces, recorded here so the
plan and the code do not disagree:

- **`identity`**: the planned principal registry overlaps `authsome`
  (authentication) and `warden` (permissions). Blackstart keeps `pkg/hooks.ActorID`
  as its own vocabulary for *who did this*, because that is a value type every
  extension embeds and it must not depend on an authentication provider. Lookup
  and authorization come from the ecosystem when there is a consumer for them;
  Phase 0 has none, and a security surface with no consumer is a liability.
- **`relay`** is webhook delivery, not the outbox. Fabriq's own relay drains
  `fabriq_outbox`, which `extensions/fabric` already runs. The two share a name
  and nothing else.

## Consequences

The audit path is stronger than planned and cost less to build. In exchange:

- Another dependency, and one whose store runs migrations of its own. It is
  pinned exactly, like everything else here.
- Audit events do not live in Fabriq, so they are not in the graph projection
  and cannot be queried alongside domain entities. That is correct rather than
  a compromise: writing audit records through `fabriq.Exec` would recurse
  through the very hook that produced them, which is why the plan already
  excluded them from that path.
- The planned in-transaction guarantee. A failed audit write rolls back the
  domain write, is not what chronicle offers by default; it batches. Where that
  guarantee is genuinely required, a `LifecycleHook` writing through `tx`
  remains available and would sit alongside chronicle rather than replace it.
  Nothing in Phase 0–2 requires it, and claiming the guarantee without
  implementing it would be worse than not having it.

## Alternatives considered

**Write the planned extension anyway.** Rejected: it would be less capable,
would need its own tamper-evidence and erasure story, and would duplicate a
maintained component from the same ecosystem.

**Use chronicle only for reads and keep a local write path.** Rejected: two
audit logs is worse than either one, because the first question anyone asks of
an audit trail is whether it is complete.
