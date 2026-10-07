## MODIFIED Requirements

### Requirement: Per-project conversation threads
Web adapters SHALL keep one server-side conversation per project, client session and system prompt: the thread key is the project plus a hash of the session id and the system turns, ignoring volatile billing header lines. A thread is reused until a turn/token budget or an upstream 404/limit forces a new one; requests marked `FullContext` MUST run on a fresh, stateless thread. The first turn of a thread SHALL carry the client's system prompt and full transcript; later turns on a live thread SHALL carry only what the thread has not seen (the new user request, or the tool results since the last reply) and MUST NOT re-send the system prompt. Resetting a project SHALL clear all of its session threads.

#### Scenario: Thread rotation
- **WHEN** a project's thread reaches its turn limit
- **THEN** the next request opens a new thread with the full flattened context

#### Scenario: New client session
- **WHEN** the user opens a new Claude Code session in a project whose earlier session has a live ChatGPT thread
- **THEN** the first request of the new session opens a new thread carrying its system prompt

#### Scenario: Side request with another system prompt
- **WHEN** Claude Code sends a title-generation request with the same session id but a different system prompt
- **THEN** it runs on its own thread and the session's main thread keeps its system prompt

#### Scenario: Follow-up on a live thread
- **WHEN** the user sends "improve it" after a finished tool session on a live thread
- **THEN** the prompt holds only that request and the tool-call cue, not the system prompt or earlier turns
