## 1. Metrics Endpoint
- [x] 1.1 Implement in-memory Prometheus metric collector in `pkg/metrics`
- [x] 1.2 Expose `GET /_am/metrics` and `GET /metrics` in `pkg/proxy/server.go`
- [x] 1.3 Add tests for Prometheus exposition format

## 2. Privacy-Safe Audit Trail
- [x] 2.1 Implement audit logger in `pkg/telemetry/audit.go` with 0600 file permissions
- [x] 2.2 Wire audit logging into `pkg/bridge/requestlog.go` on turn completion
- [x] 2.3 Add tests verifying JSONL audit entries contain metadata and no prompts

## 3. Credential Expiry & Health Diagnostics
- [x] 3.1 Implement `ParseJWTExpiry` in `pkg/auth/oauth/jwt.go`
- [x] 3.2 Implement `InspectCredentialHealth` in `pkg/identity/health.go`
- [x] 3.3 Add `amux doctor auth` subcommand in `pkg/cli/doctor.go`
- [x] 3.4 Add unit tests for JWT expiry parsing and credential health checking

## 4. Provider Capabilities
- [x] 4.1 Define `ProviderCapability` in `pkg/types/capability.go`
- [x] 4.2 Add test for capability model
