## Context

`poolSend` called `SendNamed` (exact id, no failover). `Send` held all selection logic inline.

## Goals / Non-Goals

**Goals:** family routing with the same affinity, cooldown, quarantine, tier and failover behaviour as the whole pool.
**Non-Goals:** routing by tier names (`web`, `api`); changing how exact ids behave.

## Decisions

- Extract `sendAmong(candidates, preferred, manual)` from `Send`; `SendProvider` passes the family's members (from the addressable directory, priority-sorted). A manual pin outside the family is ignored.
- Family matching is segment-aware and case-insensitive: `gemini:web` matches `gemini:web:01` but `gemini:w` matches nothing; trailing `:` / `:*` are ignored.
- An exact id is checked first so existing callers keep pin-without-failover semantics.
- The manual-only filter applies to automatic choices (pool and family), never to exact pins.

## Risks / Trade-offs

- Enforcing the manual-only filter changes behaviour for users whose manual-only accounts were (wrongly) being used automatically; this matches the documented rule.
