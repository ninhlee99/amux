## 1. Accounts
- [x] 1.1 Dedup/merge keyed by product + email (provider, identity, migration)
- [x] 1.2 Refuse to overwrite a subscription row with a web login of the same ID
- [x] 1.3 Backfill email on anonymous ChatGPT Web rows during login

## 2. Browser session
- [x] 2.1 Pick most recently used unexpired cookie across browsers/profiles

## 3. CLI
- [x] 3.1 Account table by provider + email; login prints the same table
- [x] 3.2 Resolve email / provider:email / row number; ambiguous email lists choices
- [x] 3.3 Tests for all of the above
