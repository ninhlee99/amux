# Bản đồ codebase cho AI — **repo amux** (tool gateway)

> **Scope:** Chỉ dùng khi sửa **amux-accounts** (CLI + proxy).  
> Project client qua proxy → [`PROJECT_NAVIGATION.md`](./PROJECT_NAVIGATION.md) + `am map init` → lưu `~/.am/workspaces/<project-name>/`.

> **Mục đích:** Trước khi quét cả repo amux, AI đọc file này (và `docs/ai-locate.yaml`) để biết **đi đâu trước**, **grep gì**, **test nào xác nhận**.  
> Kiến trúc sâu: [`STRUCT.md`](../STRUCT.md).

---

## 1. Quy trình bắt buộc cho agent

```
Mục tiêu người dùng
    → Tra bảng §3 hoặc grep docs/ai-locate.yaml (keywords)
    → Đọc 1–3 file trong read_first (≤400 dòng mỗi file nếu có thể)
    → Grep symbol trong package đó (§4)
    → Chạy test liên quan (§5)
    → Chỉ khi thiếu context mới mở rộng sang package lân cận
```

**Không** bắt đầu bằng `grep` toàn repo trừ khi keyword không có trong bảng §3.

---

## 2. Bản đồ tầng (runtime)

```mermaid
flowchart TB
  subgraph clients [Clients giữ nguyên CLI]
    CC[Claude Code]
    CX[Codex CLI]
    AGY[AGY / Antigravity]
    CUR[Cursor / OpenAI SDK]
  end

  subgraph gateway [Local gateway :8787]
    SRV[pkg/proxy/server.go]
    ROT[pkg/proxy/rotator.go Claude OAuth]
    BR[pkg/bridge/* wire formats]
    RTR[pkg/router/pool.go failover]
    TML[pkg/tools/* tool dialect]
  end

  subgraph backends [Backends]
    ADP[pkg/provider/* adapters]
    UP[Upstream APIs / Web sessions]
  end

  subgraph side [Sidecar]
    AM["/_am/* admin only"]
    UI[pkg/ui CLI display]
    CLI[pkg/cli commands]
  end

  CC -->|/v1/messages| SRV
  CX -->|/v1/responses /v1/chat/completions| SRV
  AGY -->|/v1beta Gemini| SRV
  CUR -->|/v1/chat/completions| SRV

  SRV --> ROT
  SRV --> BR --> RTR --> ADP --> UP
  BR --> TML
  CLI --> UI
  CLI --> SRV
  AM --- SRV
```

**Luồng dữ liệu canonical:** Mọi bridge chuyển body → `types.ChatRequest` → adapter `SendMessageStream` → `types.StreamChunk` → bridge trả wire client.

---

## 3. Tra cứu theo việc cần làm

| Bạn cần… | Đọc trước | Symbol / grep | Test |
|----------|-----------|---------------|------|
| Sửa hiển thị `am accounts` / `status` / `pool` theo group | `pkg/ui/account_groups.go`, `login.go`, `status.go` | `forEachAccountGroup`, `ResolveAccountGroup` | `pkg/ui/account_groups_test.go` |
| Session A/B/C mỗi tab một account pool | `pool.go` (Send), `guard/affinity.go`, `session_lb.go` | `ExtractSessionKey`, `pickSessionAdapter`, `Pin` | `session_lb_test.go` |
| Thứ tự failover Codex → AGY → web → API | `router/group.go`, `pool.go`, `task_prefer.go` | `GroupPriority`, `GroupOrderForRequest`, `Send` | `group_test.go`, `pool_test.go` |
| Phân loại task / thinking / Pro | `classifier.go`, `task_prefer.go` | `ClassifyTask`, `TaskKind`, `applyTaskClassification` | `classifier_test.go`, `task_prefer_test.go` |
| Claude Code tools (Bash, Read, MCP) | `bridge/claude.go`, `tools/claude.go` | `HandleClaudeMessages` | `claude_test.go`, `dialect_loop_test.go` |
| Codex tools (exec_command, Responses) | `bridge/responses.go`, `openai.go`, `tools/codex.go` | `HandleOpenAIResponses`, `DialectCodex` | `codex_loop_test.go` |
| AGY qua proxy | `bridge/gemini.go`, `cli.go` (`am run agy`) | `GOOGLE_GEMINI_BASE_URL` | `agy_loop_test.go` |
| Web backend + `<tool_call>` | `tools/webloop.go`, `provider/*_web.go` | `MaybeWrapWebStream`, `skipTextOnly` | `webloop_*_test.go` |
| Xoay Claude OAuth 5h/7d | `proxy/rotator.go`, `auth/token.go` | `Rotator`, `ForceSwitch` | `rotator_test.go` |
| Thêm/sửa provider trong pool | `provider/config.go`, `accounts.example.json` | `ProviderConfig`, `BuildAdapters` | `config_test.go` |
| `am env` / hook / launchctl | `env/env.go`, `hook/hook.go`, `launchctl.go` | `PrintEnvExports`, `SyncLaunchctlEnv` | `env_test.go`, `settings_env_test.go` |
| Guard / quarantine / 429 | `guard/*.go` | `Pace`, `IsQuarantined`, `RecordError` | `guard_test.go` |
| Endpoint `/_am/status` | `proxy/server.go`, `ui/status.go` | `newHandler`, `fetchProxyStatus` | `server_test.go` |
| Integration thật (credentials) | `live/live_test.go` | `//go:build live` | `go test -tags live ./pkg/live` |

Chi tiết machine-readable: [`docs/ai-locate.yaml`](./ai-locate.yaml).

---

## 4. Bề mặt HTTP (gateway)

| Path | Handler chính | Pool / rotator |
|------|----------------|----------------|
| `POST /v1/messages` | `bridge.HandleClaudeMessages` | toolPool + **Rotator** (Claude sub) |
| `POST /v1/chat/completions` | `bridge.HandleChatCompletions` | chatPool |
| `POST /v1/responses` | `bridge.HandleOpenAIResponses` | chatPool (Codex dialect) |
| `POST /v1beta/...` Gemini | `bridge.HandleGemini*` | chatPool |
| `GET /v1/models` | `bridge.HandleModels` | catalog |
| `/_am/status`, `/_am/guard`, `/_am/switch*` | `proxy/server.go` | JSON admin — **không** map slash Claude |

Pin provider: header `X-Provider: <id>` → `router.SendNamed`.

---

## 5. Dữ liệu & ID

| Vị trí | Nội dung |
|--------|----------|
| `~/.am/accounts.json` | Providers (có thể `AMENC1:` encrypted) — schema `accounts.example.json` |
| `~/.am/profiles/` | Snapshot CLI (`claude`, `codex`, `antigravity`) |
| ID chuẩn | `brand[:method]:NN` — `types.ParseID`, `types.FormatID` |
| Group rotate (logic) | `router/group.go` — `claude_sub`, `codex_sub`, `api_other`, `*_web` |
| UI group API | Tách `API · GEMINI` / `GROQ` từ prefix ID — `ui/apiProviderBrand` |

---

## 6. Package → câu hỏi “có phải vào đây không?”

| Package | Vào khi… |
|---------|----------|
| `pkg/cli` | Lệnh mới, flag, `am run`, help text |
| `pkg/ui` | Terminal output, không đổi routing |
| `pkg/proxy` | Route HTTP, rotator, `/_am`, lifecycle daemon |
| `pkg/bridge` | Đổi format request/response theo client |
| `pkg/router` | Ai được gọi tiếp theo, cooldown, session, task |
| `pkg/provider` | Gọi upstream cụ thể (API key, web cookie, codex token) |
| `pkg/tools` | Tên/schema tool giữa dialect |
| `pkg/guard` | Rate limit, affinity, header sanitizer |
| `pkg/profile` | Profile Claude/Codex file trên disk |
| `pkg/types` | Struct dùng chung — **không** import pkg khác |

---

## 7. Ràng buộc sản phẩm (đừng phá)

1. **Tool chạy trên máy client** (Claude Code / Codex / AGY) — proxy không exec.
2. **`/_am/*` = admin amux** — không thêm endpoint theo từng slash command Claude.
3. **Có native tool API** → bỏ qua web/Codex text-only trong pool (`skipTextOnly`).
4. **Claude IDE + pool** → không dùng `claude_sub` làm proxy backend (`IsClaudeSubscriptionGroup`).
5. **`GOOGLE_API_KEY=am-proxy`** không export global trong `am env` (tránh gcloud/Maps).

---

## 8. Test nhanh theo vùng

```bash
go test ./pkg/router/ ./pkg/ui/ ./pkg/bridge/ ./pkg/provider/ -count=1
go test -tags live ./pkg/live -count=1 -timeout 15m   # cần ~/.am thật
```

---

## 9. Cập nhật bản đồ

Khi thêm **package mới**, **route HTTP**, **lệnh CLI**, hoặc **khái niệm group/task** — cập nhật:

1. Một dòng trong bảng §3  
2. Một entry `tasks:` trong `docs/ai-locate.yaml`  
3. (Tuỳ chọn) §2 diagram nếu luồng runtime đổi  

---

*Tài liệu này là **navigation layer**; chi tiết implementation: [`STRUCT.md`](../STRUCT.md).*
