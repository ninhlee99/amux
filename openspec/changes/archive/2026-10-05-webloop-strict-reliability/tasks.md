## 1. Web-Loop Strict Reliability Refactor

- [x] 1.1 Refactor `pkg/tools/webloop.go` to eliminate forced tool synthesis from conversational refusal or incomplete text
- [x] 1.2 Retain structured markup parsing (`<tool_call>`, ````tool_call````, `[tool_call]`, `<<<AMUX_TOOL>>>`, ````json````) with schema argument coercion
- [x] 1.3 Ensure clean verbatim pass-through of assistant plain text responses when no tool markup is present

## 2. Test Verification & Suite Validation

- [x] 2.1 Update `pkg/tools/webloop_refusal_test.go` and `webloop_test.go` to verify non-forcing behavior
- [x] 2.2 Run full project test suite (`go test ./...`) to ensure all packages pass
- [x] 2.3 Run `openspec validate --strict` to verify OpenSpec compliance
