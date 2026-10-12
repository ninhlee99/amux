## 1. Routing
- [x] 1.1 `NoteAutoSwitch` replaces `SetPreferred` after automatic failover
- [x] 1.2 `bridge.SessionSwitched`; router compacts on moved sessions
- [x] 1.4 Reserve the round-robin slot at pick time; concurrency test
- [x] 1.3 Tests: auto switch does not pin; session stays on its account; detection does not pin

## 2. Subscriptions
- [x] 2.1 `identityTurnedOff` in rotator Load / refresh; test

## 3. Providers
- [x] 3.1 Gemini repeated stall → `RateLimitError` (5 min); test
- [x] 3.2 `stripChatGPTCitations`; test

## 4. Verification
- [x] 4.1 `go test ./...`
- [x] 4.2 Live: 8 new sessions spread over the pool, limited Claude Web skipped, one session keeps context over 3 turns
