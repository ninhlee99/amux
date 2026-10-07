## 1. Unreachable upstream
- [x] 1.1 `ErrUpstreamUnreachable`; ChatGPT sentinel/conversation detect Cloudflare challenges and network errors; one-line error bodies
- [x] 1.2 Health: network class never quarantines or lowers the score; plain 403 stays auth
- [x] 1.3 Router: 10s network cooldown with reason and remaining time in errors
- [x] 1.4 Claude bridge: lazy SSE start; 503 + Retry-After for unreachable
- [x] 1.5 Tests: challenge past 2KB, header detection, no quarantine, short cooldown, 503 on streaming

## 2. Thread key
- [x] 2.1 Hash only the leading system turns; test per-turn system notes keep the thread
