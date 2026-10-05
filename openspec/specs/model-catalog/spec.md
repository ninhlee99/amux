# model-catalog Specification

## Purpose
Make sure every model id amux sends or advertises exists upstream, and shape requests to what each model generation accepts.

## Requirements
### Requirement: Verified default model ids
Every default or advertised model id SHALL exist on the provider's official model page at release time; amux MUST NOT ship retired ids. As verified on 2026-10-05: Anthropic `claude-opus-5-5` (default), `claude-sonnet-5-5` (gateway default for Claude Code); OpenAI and Codex `gpt-6-astra`, `gpt-6.1-sol` (default for Codex and generic OpenAI-compatible accounts), `gpt-6-luna` (ChatGPT); Gemini `gemini-3.8-flash` (flash) and `gemini-3.1-pro-preview` (pro); Groq `llama-3.3-70b-versatile`; xAI `grok-4.7`; Kimi `kimi-k2.7-code` on `https://api.moonshot.ai/v1`.

#### Scenario: Retired id in source
- **WHEN** a non-test source file contains a retired id such as `"gpt-4o"` or `"claude-3-7-sonnet-20250219"`
- **THEN** the test suite fails

### Requirement: Per-generation Claude parameters
Requests to Claude SHALL use adaptive thinking on 4.6+ non-Haiku models (no `budget_tokens`), omit sampling parameters from the 4.7 generation on, and downgrade forced `tool_choice` (`any`/`tool`) to `auto` on Opus 5.5+, Sonnet 5.5+ and Fable/Mythos 5.1+; older models keep their previous request shape.

#### Scenario: Opus 5.5 with thinking and temperature
- **WHEN** a client asks for thinking with a budget and `temperature: 0.7` on `claude-opus-5-5`
- **THEN** the upstream payload has `thinking: {type: "adaptive"}` and no `temperature`

### Requirement: Retired services are not offered
amux MUST NOT create accounts for retired services; `amux login github` SHALL explain that GitHub Models was retired on 2026-07-30 and suggest alternatives.

#### Scenario: GitHub Models login
- **WHEN** the user runs `amux login github`
- **THEN** no account is saved and the retirement notice is printed

