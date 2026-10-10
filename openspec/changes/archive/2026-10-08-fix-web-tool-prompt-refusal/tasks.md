## 1. Web Tool Prompt Framing Refactoring

- [x] 1.1 Refactor `webToolPreamble`, `webToolReminder`, and `webToolCloser` in `pkg/tools/webloop.go` to use neutral protocol framing without coercive phrases
- [x] 1.2 Refactor `midTaskCue`, `newRequestCue`, and closer cues in `pkg/provider/prompt.go` to maintain clear positive guidance without triggering safety filters
- [x] 1.3 Expand `IsToolRefusal` in `pkg/tools/webloop.go` with simulation and harness refusal patterns

## 2. Verification and Regression Testing

- [x] 2.1 Update existing unit and emulation tests in `pkg/tools/` and `pkg/provider/` to match updated prompt structures
- [x] 2.2 Add unit tests verifying Claude Web simulation refusal detection and ChatGPT / Gemini compatibility
- [x] 2.3 Run full test suites across `pkg/tools/...`, `pkg/provider/...`, and `pkg/bridge/...` to ensure zero regressions
