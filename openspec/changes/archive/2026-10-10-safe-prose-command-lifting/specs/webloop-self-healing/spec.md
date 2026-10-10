## ADDED Requirements

### Requirement: Commands lifted from prose are read-only
The gateway SHALL NOT turn a command written in a web reply's prose (inline backticks or a bash fence, outside `<tool_call>`) into a tool call unless the reply is a tool refusal or a stall, and the command only reads (for example `git status`, `git diff`, `ls`, `cat`), with no redirection or command substitution.

#### Scenario: Prose mentions a forbidden command
- **WHEN** during `/open-pr:fix` a web reply says "`git add -A` / `git add .` is forbidden"
- **THEN** no tool call is produced

#### Scenario: Stall names a read-only command
- **WHEN** a reply says "I will start by running `git status`" with no `<tool_call>`
- **THEN** a Bash call running `git status` is produced
