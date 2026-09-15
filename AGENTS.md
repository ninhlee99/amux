# Hướng dẫn agent — `amux`

## Đang sửa repo nào?

| Workspace | Đọc trước |
|-----------|-----------|
| **Repo amux** (module `amux-accounts`) | [docs/AI_CODEBASE_MAP.md](docs/AI_CODEBASE_MAP.md) → [docs/ai-locate.yaml](docs/ai-locate.yaml) |
| **Project client** (app khác, traffic qua proxy) | `~/.am/workspaces/<project-name>/` — `init` (full) / `recent`+`learn` (hẹp). [docs/PROJECT_NAVIGATION.md](docs/PROJECT_NAVIGATION.md). |

Không dùng bản đồ amux để navigate codebase client.

**Check/update (không init):** `am map recent --needs-learn` → đọc source hẹp → `am map learn`. Cấm dump full MODULES.

## Sửa amux (gateway)

1. [docs/AI_CODEBASE_MAP.md](docs/AI_CODEBASE_MAP.md)
2. [docs/ai-locate.yaml](docs/ai-locate.yaml)
3. [STRUCT.md](STRUCT.md) — chi tiết kiến trúc

Entry: `main.go` → `pkg/cli/cli.go`. Gateway: `pkg/proxy/server.go` (`:8787`).
