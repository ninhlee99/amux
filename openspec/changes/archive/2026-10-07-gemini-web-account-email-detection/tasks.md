## 1. Gemini Account Extraction Module

- [x] 1.1 Implement `ParseGeminiAccountEmail` and `ParseGeminiAccountPlan` in `pkg/browser/gemini_account.go`
- [x] 1.2 Implement `FetchGeminiAccount(sessionKey, cookieHeader string)` in `pkg/browser/gemini_account.go`
- [x] 1.3 Add unit tests in `pkg/browser/gemini_account_test.go` covering WIZ data, aria-label, and plan detection

## 2. Login Flow and Identity Integration

- [x] 2.1 Update `loginGeminiWeb` in `pkg/ui/login.go` to call `FetchGeminiAccount`, announce email & plan, and use `savePoolLogin`
- [x] 2.2 In `loginGeminiWeb`, auto-extract `__Secure-1PSIDTS` alongside `__Secure-1PSID` from local browsers
- [x] 2.3 Implement `backfillGeminiAccounts()` in `pkg/ui/login.go` to backfill email on legacy anonymous rows
- [x] 2.4 Update `syncAddressableAdaptersToIdentities` in `pkg/cli/id.go` to populate email from `p.Account`

## 3. Verification and Testing

- [x] 3.1 Run unit tests `go test ./pkg/browser/... ./pkg/ui/... ./pkg/cli/...`
- [x] 3.2 Run full repo test suite `go test ./...`
- [x] 3.3 Validate change with `openspec validate --strict`
