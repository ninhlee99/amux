# Hướng dẫn agent — `amux`

## Đang sửa repo nào?

| Workspace | Đọc trước |
|-----------|-----------|
| **Repo amux** (module `amux-accounts`) | [docs/AI_CODEBASE_MAP.md](docs/AI_CODEBASE_MAP.md) → [docs/ai-locate.yaml](docs/ai-locate.yaml) |
| **Client** (app qua proxy) | `~/.am/workspaces/<name>/` — `init` / `recent`+`learn`. [docs/WORKSPACE_MAP.md](docs/WORKSPACE_MAP.md). |

Không dùng bản đồ amux để navigate codebase client.

**Check/update:** `am map recent` → GRAPH (mesh+1 subnet) → `am map learn`. Cấm dump bảng func.

## Sửa amux (gateway)

1. [docs/AI_CODEBASE_MAP.md](docs/AI_CODEBASE_MAP.md)
2. [docs/ai-locate.yaml](docs/ai-locate.yaml)
3. [STRUCT.md](STRUCT.md) — chi tiết kiến trúc

Entry: `main.go` → `pkg/cli/cli.go`. Gateway: `pkg/proxy/server.go` (`:8787`).
