# Hướng dẫn agent — `amux`

## Đang sửa repo nào?

| Workspace | Đọc trước |
|-----------|-----------|
| **Repo amux** (module `amux-accounts`) | [docs/AI_CODEBASE_MAP.md](docs/AI_CODEBASE_MAP.md) → [docs/ai-locate.yaml](docs/ai-locate.yaml) |

## Sửa amux (Universal AI Gateway)

1. [docs/AI_CODEBASE_MAP.md](docs/AI_CODEBASE_MAP.md)
2. [docs/ai-locate.yaml](docs/ai-locate.yaml)
3. [STRUCT.md](STRUCT.md) — Chi tiết kiến trúc phân tầng AMUX

- Entry: `main.go` → `pkg/cli/cli.go`. Gateway: `pkg/proxy/server.go` (`:8787`).
- Cấu hình & lưu trữ: `~/.amux/` (`identities.json`, `accounts.json`, `config.json`).
- CLI chính thức: `amux` (`amux start`, `amux status`, `amux switch`, `amux account`, `amux hook`, `amux doctor`, `amux env`).
- Chế độ hoạt động: Web accounts (Gemini Web, ChatGPT Web) làm AI Proxy cho Claude Code CLI, Cursor, Codex, Antigravity. Không tự ý bật các account subscription.
