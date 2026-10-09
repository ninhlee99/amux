## 1. Fix

- [x] 1.1 `HTTP2Config{SendPingTimeout: 30s, PingTimeout: 15s}` on web transports
- [x] 1.2 60s deadline on the ChatGPT sentinel request

## 2. Verification

- [x] 2.1 `go vet`, `go test ./...`; live `/open-pr:fix` through ChatGPT Web
