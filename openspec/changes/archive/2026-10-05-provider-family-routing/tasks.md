## 1. Routing

- [x] 1.1 Extract `sendAmong` from `Send`; enforce `canAutoRotate` in automatic selection
- [x] 1.2 Add `SendProvider` and `FamilyMembers`
- [x] 1.3 Use `SendProvider` for `X-Provider` and `amux_ask.provider`

## 2. Tests and docs

- [x] 2.1 Router tests (family failover, wildcard/case, exact pin, no match, manual-only)
- [x] 2.2 HTTP test with `X-Provider: gemini:web`
- [x] 2.3 Update specs, README and tool descriptions
