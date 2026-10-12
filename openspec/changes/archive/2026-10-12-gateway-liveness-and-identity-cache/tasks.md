## 1. Implementation
- [x] 1.1 identities plaintext cache; test
- [x] 1.2 patient probe, no pid write in Start
- [x] 1.3 strong+weak relay-doubt signals; tests

## 2. Verification
- [x] 2.1 `go test ./...`
- [x] 2.2 Live: status 700ms → 15ms; 4 parallel sessions with no spurious gateway start; injection-flagging reply on Claude Web passed through
