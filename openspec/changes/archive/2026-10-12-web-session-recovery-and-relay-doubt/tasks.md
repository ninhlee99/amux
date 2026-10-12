## 1. Session recovery

- [x] 1.1 `ExtractCookies`, `ExtractCookieFromStore`
- [x] 1.2 `pickBrowserSession` in ChatGPT / Claude / Gemini refresh
- [x] 1.3 Tests: account-matching pick, no-match auth error

## 2. Relay doubt

- [x] 2.1 `IsRelayDoubt`, `ResetsThread` at adapter reset points
- [x] 2.2 `sendWithWebNudge` plain retry, error on second doubt; tests

## 3. Verification

- [x] 3.1 `go test ./...`
- [x] 3.2 Live `--matrix` on gemini:web:bi117ute, chatgpt:bi117ute, claude:web:bi117ute, chatgpt:ninhle21199
