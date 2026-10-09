## 1. Prompt and catalog

- [x] 1.1 Reframe ChatGPT preamble/reminder/closer as a text protocol
- [x] 1.2 Mark optional catalog args with `?`, list enum values
- [x] 1.3 Drop optional args that are null or outside their enum in `coerceToolArgs`
- [x] 1.4 Skip skill-expansion turns in `currentUserTask`; `[xfer]` only with >1 non-system turn

- [x] 1.5 Head skill bodies with a "carry these out with <tool_call>s" note
- [x] 1.6 Collapse Gemini Web self-links to bare URLs

## 2. Self-healing

- [x] 2.1 `IsToolStall` detector
- [x] 2.2 `sendWithWebNudge` on Claude/ChatGPT/Gemini web adapters
- [x] 2.3 `ChatRequest.ClientMessages` + `ClientHistoryMark` for thread checkpoints
- [x] 2.4 Refusal kickstart prefers named, runnable tools; "mcp" no longer disables Bash when a non-MCP tool is named

## 3. Verification

- [x] 3.1 Unit tests: catalog marking, enum drop, skill task cue, stall detection, nudge retry, hermetic `SplitMCPServerTool`
- [x] 3.2 `go test ./...`
- [x] 3.3 Live probe through `amux run claude -p` on ChatGPT Web and Gemini Web
