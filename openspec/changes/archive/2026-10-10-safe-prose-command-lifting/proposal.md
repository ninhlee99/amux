## Why

During `/open-pr:fix` through Claude Web, the model wrote prose such as "`git add -A` / `git add .` is forbidden". Because the task was a skill, `FinalizeWebToolCalls` lifted the backticked text into a Bash call, hid the prose, and ran `git add` — six times. Claude Web saw tool results it never requested, concluded the feed was being manipulated, and abandoned the fix. Any command in prose or an example fence (`git push`, `rm -rf`) could run the same way.

## What Changes

- Commands are lifted from prose (bash fences, inline backticks) only when the reply is a refusal or a stall, not merely because the task is a skill.
- A lifted command — and a bare first-turn ```bash fence — runs only if it is read-only: `git status|diff|log|show|branch|remote|rev-parse|ls-files|grep|blame`, `ls`, `cat`, `head`, `tail`, `grep`, `rg`, `find` (no `-delete`/`-exec`), `wc`, `pwd`, `echo`; no redirection or substitution.
- Stall detection also recognises "Let's …" / "Let us …".

## Capabilities

### Modified Capabilities
- `webloop-self-healing`: safe command lifting.

## Impact

- `pkg/tools/webloop.go`.
