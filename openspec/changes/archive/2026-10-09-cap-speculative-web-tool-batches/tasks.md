## 1. Cap

- [x] 1.1 Prompt: independent calls only, nothing written before the result
- [x] 1.2 `maxWebToolCallsPerTurn` = 5 in `wrapWebStream`; `StreamChunk.SpeculativeTools`
- [x] 1.3 `sendWithWebNudge` drops held text of a speculative turn
- [x] 1.4 `IsLostTask` mid tool loop is nudged like a stall

## 2. Verification

- [x] 2.1 Unit tests; `go test ./...`
- [x] 2.2 Live `/open-pr:review` through Gemini Web
