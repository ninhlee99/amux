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
- CLI chính thức: `amux` (`amux login`, `amux run <ide>`, `amux dashboard`, `amux start`, `amux status`, `amux switch`, `amux account`, `amux pool`, `amux off`, `amux doctor`, `amux mcp`, `amux config secret-store`).
- Spec: `openspec/specs/` (OpenSpec). Thay đổi hành vi → tạo change trong `openspec/changes/`, `openspec validate --strict`, archive khi xong.
- Chế độ hoạt động: Sử dụng `amux run <claude|cursor|agy|codex>` để chạy IDE trong môi trường Sandbox cô lập trỏ qua Gateway :8787 (không ô nhiễm shell hay launchctl). Chạy `claude`/`cursor` trực tiếp sẽ chạy Native 100%. Web accounts (Gemini Web, ChatGPT Web, Claude Web) làm AI Proxy cho IDEs — hoặc làm tool MCP (`amux mcp`) cho mọi agent hỗ trợ MCP. Không tự ý bật các account subscription: subscription chỉ vào pool xoay khi người dùng chạy `amux pool add`.
