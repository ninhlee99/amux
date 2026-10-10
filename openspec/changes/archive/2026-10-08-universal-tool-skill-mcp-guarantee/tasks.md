## 1. Skill and Reminder Metadata Normalization

- [x] 1.1 Implement `CleanUserTurnContent` in `pkg/tools/webloop.go` to strip `<system-reminder>` wrappers and ignore metadata-only turns
- [x] 1.2 Update `hasUserTurn`, `currentUserTask`, and `endsWithUserRequest` in `pkg/provider/prompt.go` to leverage `tools.CleanUserTurnContent`
- [x] 1.3 Update `lastUserPrompt` in `pkg/provider/claude_web.go` to filter out metadata-only turns

## 2. Multi-Tier Fallback Tool Call Recovery in pkg/tools

- [x] 2.1 Update `FinalizeWebToolCalls` in `pkg/tools/webloop.go` to recover commands from markdown codeblocks (`reBashFence`) when `calls` is empty
- [x] 2.2 Recover commands from inline backticks (`reBacktickCmd`) regardless of refusal status
- [x] 2.3 Add agentic skill task detection and auto-kickstart (`git status`) when models produce conversational prose on skill tasks

## 3. Verification and Testing

- [x] 3.1 Add unit tests in `pkg/provider/prompt_test.go` verifying skill multi-turn persistence across `<system-reminder>` metadata turns
- [x] 3.2 Add unit tests in `pkg/tools/webloop_test.go` verifying markdown codeblock fallback and skill auto-kickstart
- [x] 3.3 Run full test suite across all packages to guarantee zero regressions and build `amux` CLI
