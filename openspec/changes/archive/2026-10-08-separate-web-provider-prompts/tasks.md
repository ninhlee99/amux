## 1. Provider-Specific Prompt Implementations in pkg/tools

- [x] 1.1 Implement distinct preamble, reminder, and closer templates for `claude`, `chatgpt`, and `gemini` in `pkg/tools/webloop.go`
- [x] 1.2 Implement `WebPreambleForProvider`, `WebCatalogOnlyForProvider`, and `WebCloserForProvider` functions while preserving generic wrappers

## 2. Web Provider Prompt Routing in pkg/provider

- [x] 2.1 Implement `WebBackendPromptForProvider` in `pkg/provider/prompt.go`
- [x] 2.2 Wire `ClaudeWebAdapter` in `pkg/provider/claude_web.go` to use `"claude"`
- [x] 2.3 Wire `ChatGPTWebAdapter` in `pkg/provider/chatgpt_web.go` to use `"chatgpt"`
- [x] 2.4 Wire `GeminiWebAdapter` in `pkg/provider/gemini_web.go` to use `"gemini"`

## 3. Verification and Testing

- [x] 3.1 Add unit tests verifying distinct prompt rendering per provider
- [x] 3.2 Run test suite across all packages to guarantee zero regressions
