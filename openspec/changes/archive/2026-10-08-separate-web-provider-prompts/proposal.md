## Why

Currently, all web providers (Claude Web, ChatGPT Web, Gemini Web) share a single global prompt preamble, reminder, and closer in `WebBackendPrompt` and `pkg/tools/webloop.go`. Each web provider has distinct operational characteristics:
- **ChatGPT Web** needs specific warnings to prevent using its built-in Python/container sandbox rather than the repository workspace.
- **Claude Web** has no container sandbox, and mentions of external sandboxes or rigid steering can trigger alignment/jailbreak suspicion; it performs best with clean, natural agent role definition and tool call instructions.
- **Gemini Web** operates via StreamGenerate and benefits from concise workspace tool directives aligned with Google model conventions.

Separating prompt construction into dedicated, provider-tailored prompts ensures each model receives optimal framing without leaking irrelevant constraints from other models.

## What Changes

- **Provider-Aware Prompt Architecture**: Introduce `WebBackendPromptForProvider`, `WebPreambleForProvider`, `WebCatalogOnlyForProvider`, and `WebCloserForProvider`.
- **Dedicated Provider Prompts**:
  - `claude`: Natural assistant role with `<tool_call>` instructions; no mention of Python/container sandboxes.
  - `chatgpt`: Retains Python/container sandbox steering and bracket/xml tool call examples.
  - `gemini`: Clean Google-friendly workspace tool instructions.
- **Adapter Integration**: Update `claude_web.go`, `chatgpt_web.go`, and `gemini_web.go` to invoke `WebBackendPromptForProvider` with their respective provider identifier.
- **Backward Compatibility**: Preserve existing `WebBackendPrompt`, `WebPreamble`, `WebCatalogOnly`, and `WebCloser` signatures by delegating to generic/default implementations.

## Capabilities

### Modified Capabilities
- `web-tool-optimization`: Add provider-specific prompt specialization for Claude Web, ChatGPT Web, and Gemini Web.

## Impact

- `pkg/tools/webloop.go`: Add provider-specific preamble, reminder, and closer functions while preserving backwards-compatible wrappers.
- `pkg/provider/prompt.go`: Add `WebBackendPromptForProvider` and wire adapters.
- `pkg/provider/claude_web.go`, `pkg/provider/chatgpt_web.go`, `pkg/provider/gemini_web.go`: Pass provider identity when constructing web prompts.
- Unit tests: Add unit tests validating provider-specific prompt rendering.
