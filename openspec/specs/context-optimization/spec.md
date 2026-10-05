# context-optimization Specification

## Purpose
Keep prompts within what each backend accepts without breaking prompt caches on subscription accounts.

## Requirements

### Requirement: Cache-preserving for subscriptions
For subscription backends amux MUST keep message content and order unchanged so upstream prompt caching keeps working across account switches.

#### Scenario: Switch between two Claude subscriptions
- **WHEN** a session moves from one subscription to another
- **THEN** the forwarded history is byte-identical

### Requirement: Budgeted shrinking for web backends
For web backends amux SHALL fit conversations into the web budget (about 20k tokens, hard cap 85k runes) by trimming large tool results and deduplicating repeated calls while keeping the system prompt, the first user goal and the recent tail.

#### Scenario: Oversized history
- **WHEN** a Claude Code history exceeds the web budget
- **THEN** the prompt sent to the web backend is within budget and still contains the original task
