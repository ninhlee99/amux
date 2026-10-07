## 1. Delta-only live threads
- [x] 1.1 Decide the tool-session path on the unshrunk history
- [x] 1.2 Live threads send only the new request / tool results, never the system prompt
- [x] 1.3 Shrink only fresh-thread and FullContext transcripts
- [x] 1.4 Tests: follow-up after shrink, no system re-send, fresh thread keeps system

## 2. Thread per session and system prompt
- [x] 2.1 `ThreadKey(req)`: project + hash(session id, system turns without billing header)
- [x] 2.2 Per-thread snapshot files; project reset clears every session thread
- [x] 2.3 ChatGPT, Claude.ai and Gemini web adapters key threads with `ThreadKey`
- [x] 2.4 Tests: sessions isolated, side request isolated, billing header ignored, project reset
