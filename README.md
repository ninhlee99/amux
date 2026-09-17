<p align="center">
  <img src="assets/logo.svg" alt="amux logo" width="160" />
</p>

<h1 align="center">amux (<code>am</code>)</h1>

<p align="center">
  <b>AI Account Multiplexer & Local Failover Gateway</b><br>
  Tự động xoay vòng tài khoản Claude Code chống Rate Limit & Cổng AI Gateway chuẩn OpenAI / Anthropic / Gemini đa nhà cung cấp.
</p>

<p align="center">
  <a href="#-tính-năng-cốt-lõi">Tính Năng</a> •
  <a href="#-cài-đặt-nhanh">Cài Đặt</a> •
  <a href="#-danh-sách-lệnh-cli-amux--am">Lệnh CLI</a> •
  <a href="#-local-ai-gateway-http1270018787">AI Gateway</a> •
  <a href="#-luồng-claude-code--proxy--tools">Claude Code & Tools</a> •
  <a href="#-antigravity--agy-google-genai">Antigravity (AGY)</a> •
  <a href="#️-cấu-hình-provider-pool-amaccountsjson">Provider Pool</a> •
  <a href="STRUCT.md">Kiến Trúc</a>
</p>

---

## ⚡ Tính Năng Cốt Lõi

- 🔄 **Auto-Rotate Claude Accounts:** Tự động phát hiện và xoay vòng qua nhiều tài khoản Claude Pro / Max trước khi chạm rate limit (dựa vào header `anthropic-ratelimit-*`, ngưỡng mặc định 95%), tự động refresh token OAuth.
- 🌐 **Local AI Gateway (`:8787`):** Hỗ trợ đầy đủ 3 giao thức chuẩn:
  - **OpenAI:** `/v1/chat/completions`, `/v1/models` (cho Cursor, Continue, Cline, LangChain, SDK)
  - **Anthropic:** `/v1/messages` (cho Claude Code)
  - **Google Gemini:** `/v1beta/models/...`, `:generateContent`, `:streamGenerateContent`, `:countTokens` (cho Antigravity / AGY CLI & IDE)
- 🧰 **Bi-Directional Tool Mid-Layer (`pkg/tools`, `pkg/bridge`):** Chuyển đổi công cụ hai chiều 3 phía giữa Claude Code (`tool_use`), Codex CLI (`function_call`), và Antigravity / Gemini (`functionCall`). Tự động remap các công cụ hệ thống (Bash, Read, Write, Edit, Grep, Find), Subagents (`Agent` ↔ `invoke_subagent`), và MCP Tools (`mcp__*` ↔ `call_mcp_tool`) kèm cơ chế tự ép kiểu an toàn (auto type-coercion).
- 🗜️ **20k Token Budget & Context Shrink (`pkg/ctxshrink`):** Engine nén ngữ cảnh tiến trình 3 cấp độ với trần an toàn `20.000 tokens` và chặn cứng `85.000 runes`. Giữ nguyên User Goal và các lượt hội thoại gần nhất (tail turns), bảo vệ tuyệt đối không bao giờ bị lỗi `413 Payload Too Large` trên ChatGPT Web, Claude Web và Gemini Web.
- 🤖 **WebLoop Tool Emulation (`webloop.go`):** Giả lập giao thức gọi công cụ `<tool_call>` cho các Web Session (ChatGPT Web, Claude Web, Gemini Web), cho phép backend trình duyệt tham gia vào vòng lặp agent loop và gọi công cụ mượt mà.
- 🧠 **Task Classifier & Smart Escalation (`classifier.go`):** Tự động phân loại ngữ cảnh tác vụ (kiến trúc hệ thống, đánh giá an ninh bảo mật, phân tích crash dump, tối ưu hiệu năng) để tự động kích hoạt chế độ suy luận chuyên sâu (*extended thinking*) hoặc điều hướng lên các model Pro (như `gemini-3.1-pro`, `o3-mini`).
- 🛡️ **Multi-Provider Failover:** Chuyển mạch thông minh khi gặp 429 hoặc lỗi xác thực giữa GitHub Models, Google Gemini API, Groq, OpenRouter, Codex CLI và Web Sessions (ChatGPT / Claude / Gemini) với thời gian chờ (cooldown) 30 phút.
- 🔀 **Quản lý Pool linh hoạt + `X-Provider`:** `am off` / `am pool remove` đưa tài khoản ra khỏi vòng xoay; `am on` / `am pool add` đưa trở lại. Gọi trực tiếp bằng header `X-Provider: <id>` (+ optional `X-Model`).
- 🧠 **Context & Session Retention:** Duy trì lịch sử hội thoại xuyên suốt khi chuyển đổi giữa các tài khoản hoặc khi failover sang provider khác.
- 📊 **Codex-Style Statusline & Token Analytics:** Footer trực quan hiển thị số token context của session (`tok`), thanh phần trăm hạn ngạch `5h` và `7d` (Claude), thanh quota đa tầng (AGY/Codex); thống kê toàn diện qua `am usage`.
- 🪵 **Error Diagnostics & 7-Day Log Retention (`am logs`):** Lưu trữ chẩn đoán chi tiết lỗi turn (`errors.log`), tự động dọn dẹp nhật ký sau 7 ngày; hỗ trợ tạo issue báo lỗi an toàn với `am feedback`.
- 🧼 **Privacy Redact (`pkg/privacy`):** Tự động che mờ email, API key, webhook, token, thẻ tín dụng trên payload outbound trước khi gửi lên upstream.
- 🛡️ **Anti-Ban Guard & Account Health (`pkg/guard`):** 5 lớp bảo vệ tài khoản: Header Sanitizer chống rò rỉ proxy; Traffic Pacing & Micro-Jitter phá vỡ pattern bot; Health Score (0-100) kèm Auto-Quarantine cách ly an toàn khi gặp lỗi xác thực/429 liên tiếp; Session Affinity ghim thread cố định; Egress Proxy (HTTP/SOCKS5) riêng biệt cho từng tài khoản.
- 🚇 **HTTP CONNECT Tunneling:** Đóng vai trò forward proxy hỗ trợ các công cụ hoặc SDK cấu hình qua biến `HTTP_PROXY` / `HTTPS_PROXY`.
- 🔐 **Bảo Mật Cao Cấp:** Lưu trữ chứng thực trong macOS Keychain; mã hóa tệp `accounts.json` bằng AES-256-GCM (`AMENC1:`) với Scrypt master key; khi bind `--public` tự động phát hành API Key tạm thời `amux-<auth-token>` (`am proxy token`).
- 🔁 **Đồng Bộ Môi Trường Toàn Diện:** Tự động đồng bộ biến môi trường giữa `~/.claude/settings.json`, macOS GUI session qua `launchctl`, và shell qua `eval "$(am env)"` (tự động `unset` khi proxy tắt để trả về upstream gốc).

---

## 🚀 Cài Đặt Nhanh

### 1. Cài đặt tự động (macOS):
```sh
curl -fsSL https://raw.githubusercontent.com/ninhlee99/amux/main/install.sh | sh
```
> *Yêu cầu: **macOS**, **Go 1.26+**, Git. Script biên dịch từ source, cài đặt song song hai lệnh **`amux`** và **`am`** vào `~/.local/bin` (hoặc `/usr/local/bin`), sau đó tự động chạy `am setup` để tích hợp hooks và lệnh slash `/am:feedback`.*

### 2. Sử dụng ngay với Claude Code:
1. Mở `claude` — hook `SessionStart` sẽ tự động khởi động `am proxy up`. Proxy tự động snapshot tài khoản Claude hiện tại vào `~/.am/` (hoặc bạn có thể dùng `am add` thủ công).
2. Thêm tài khoản phụ: trong Claude Code gõ `/login` với tài khoản mới, sau đó chạy `am add` (hoặc mở một tab mới — proxy sẽ tự động nhận diện và snapshot).
3. Thêm các provider dự phòng vào pool: `am login chatgpt|claude|gemini|gemini-web|github|groq|kimi|grok` hoặc `am api add`.

* **Tự động cập nhật:** `am setup --auto-update` (Cài đặt LaunchAgent kiểm tra bản mới định kỳ).
* **Nâng cấp thủ công:** `am update` (hoặc `am update --force` để build lại từ mã nguồn mới nhất; giữ nguyên toàn bộ dữ liệu tại `~/.am/`). Nếu máy chưa có `go`/`git`, `am update` tự động cài đặt hộ (qua Homebrew hoặc tải binary chính thức từ go.dev) rồi tiếp tục build — **không cần chạy lại lệnh `curl ... | sh`**.
* **Gỡ cài đặt:** `am uninstall` — gỡ hooks khỏi mọi IDE/CLI đã tích hợp, tắt proxy daemon & auto-update, xoá binary (`am`/`amux`); dữ liệu tài khoản tại `~/.am/` được giữ nguyên. Dùng `am uninstall --purge` để xoá luôn cả `~/.am/`.

---

## 📋 Danh Sách Lệnh CLI (`amux` / `am`)

> 💡 **Mẹo:** Bạn có thể dùng `amux` hoặc `am` thay thế cho nhau (ví dụ: `amux sw` hoàn toàn tương đương với `am sw`).

### 1. Thao Tác Hàng Ngày (Daily Workflow)
| Lệnh | Mô Tả |
| :--- | :--- |
| `am status` (hoặc `am st`) | Kiểm tra trạng thái cổng gateway, provider đang active, quota & số phiên kết nối |
| `am ls [filter]` / `am accounts` | Liệt kê tất cả tài khoản, độ ưu tiên priority và trạng thái tham gia vòng xoay (`POOL=IN/OUT`) |
| `am sw [id]` / `am switch` | Mở picker tương tác chọn nhanh hoặc ghim cố định một profile Claude / Provider |
| `am on <id>` / `am off <id>` | Bật / tắt nhanh một tài khoản tham gia vào rotation pool |
| `am run <tool> [args...]` | Khởi chạy công cụ (`claude`, `codex`, `agy`) kèm tự động nạp môi trường proxy |

### 2. Quản Lý Tài Khoản & Providers
| Lệnh | Mô Tả |
| :--- | :--- |
| `am add [tool] [tên]` | Lưu thông tin đăng nhập CLI hiện tại trên máy (`claude`, `codex`, `gemini`) |
| `am login <provider>` | Đăng nhập session web tương tác: `chatgpt`, `claude`, `gemini`, `gemini-web`, `grok`... |
| `am oauth <provider> [tên]` | **Standalone OAuth:** Đăng nhập trực tiếp qua OAuth (`claude`, `codex`, `antigravity`, `kimi`...) |
| `am api add <tên> [flags]` | Thêm OpenAI-compatible endpoint (`--endpoint <url> --api-key <key> [--model M] [--priority N]`) |
| `am rm <id\|name>` | Xoá tài khoản hoặc provider (hỗ trợ `am restore <id>` để phục hồi từ thùng rác) |
| `am rename <cũ> <mới>` | Đổi tên hồ sơ tài khoản |

### 3. Cổng AI Gateway & Proxy Control
| Lệnh | Mô Tả |
| :--- | :--- |
| `am proxy up [flags]` | Khởi động proxy daemon nền (`127.0.0.1:8787`). Thêm `--public` để bind `0.0.0.0` chia sẻ LAN |
| `am proxy down [flags]` | Tắt proxy daemon. Thêm `--public` để tắt chế độ chia sẻ LAN, thu hồi token và revert về `127.0.0.1` |
| `am proxy token` | Xem hoặc sinh mã khóa bảo vệ (`admin auth token`) khi chạy proxy public |
| `am env [--public]` | Xuất lệnh cấu hình môi trường `eval "$(am env)"` (`--public` dùng địa chỉ IP LAN) |
| `am guard [reset]` | Giám sát điểm sức khỏe (0-100), cách ly (Quarantine), reset cooldown anti-ban |

### 4. Điều Tuyến Nâng Cao & Routing Pool
| Lệnh | Mô Tả |
| :--- | :--- |
| `am pool` | Xem danh sách các provider đang tham gia vòng xoay (**IN**) |
| `am pool set <id> [flags]` | Cấu hình tham số provider: `--priority N`, `--model M`, `--on`, `--off` |
| `am doctor providers` | Gửi truy vấn thử nghiệm (1-turn probe) kiểm tra tình trạng kết nối từng adapter |

### 5. Thống Kê & Tiện Ích Mở Rộng
| Lệnh | Mô Tả |
| :--- | :--- |
| `am usage [day\|week\|all]` | Báo cáo chi tiết lượng token đã tiêu thụ và hạn mức theo từng model/project |
| `am logs [--errors] [--clean]` | Quản lý nhật ký hoạt động: `--errors` (xem lỗi), `--clean` (dọn dẹp log > 7 ngày) |
| `am setup [--auto-update]` | Cài đặt hooks, lệnh `/am:feedback`, kích hoạt tự động cập nhật |
| `am update [--force]` | Nâng cấp amux lên bản mới nhất từ GitHub; tự động cài `go`/`git` nếu thiếu (không cần chạy lại lệnh `curl \| sh`) |
| `am uninstall [--purge]` | Gỡ hooks, tắt proxy/auto-update, xoá binary; `--purge` xoá luôn dữ liệu tài khoản tại `~/.am/` |
| `am export` / `am import` | Đóng gói xuất / nhập bộ hồ sơ tài khoản mã hóa (`.amexp`) qua máy khác |
| `am feedback [--error]` | Mở issue báo lỗi GitHub, tự động thu thập thông tin chẩn đoán đã khử dữ liệu nhạy cảm |

---

## 🔌 Local AI Gateway (`http://127.0.0.1:8787`)

Cổng gateway amux đóng vai trò máy chủ trung gian thống nhất:
- **OpenAI Endpoint:** `/v1/chat/completions`, `/v1/models`
- **Anthropic Endpoint:** `/v1/messages`
- **Gemini Endpoint:** `/v1beta/models/...`, `:generateContent`, `:streamGenerateContent`, `:countTokens`
- **Forward Proxy:** Hỗ trợ HTTP CONNECT tunneling qua biến `HTTP_PROXY` / `HTTPS_PROXY`.
- **Admin (`/_am/*`):** `/_am/status`, `/_am/guard`, `/_am/guard/reset`, `/_am/sync`, …

Khi chạy trên loopback (`127.0.0.1`), proxy **không** bắt buộc API key (chấp nhận bất kỳ token mẫu/dummy). Khi gắn cờ `--public` (lắng nghe `0.0.0.0`), hệ thống sẽ kích hoạt bảo mật nghiêm ngặt và tự sinh API Key tạm thời dạng `amux-<auth-token>`. Các client ngoài mạng bắt buộc phải truyền key này qua header `Authorization: Bearer <key>`, `X-Api-Key` hoặc `X-Am-Token`. Bạn có thể in key này bằng lệnh `am proxy token`.

### 1. Claude Code
```sh
am proxy up
eval "$(am env)"    # Thiết lập ANTHROPIC_BASE_URL=http://127.0.0.1:8787 và ANTHROPIC_AUTH_TOKEN=am-proxy
claude              # Hoặc chạy nhanh: am run claude
```

### 2. Cursor / Continue / Cline / LangChain
Cấu hình trong file môi trường `.env` hoặc settings của extension:
```env
OPENAI_BASE_URL="http://127.0.0.1:8787/v1"
OPENAI_API_KEY="am-proxy"
```

### 3. Antigravity / Gemini CLI / Google Gen AI SDK
```env
GEMINI_API_BASE="http://127.0.0.1:8787"
GOOGLE_GENAI_BASE_URL="http://127.0.0.1:8787"
```

### 4. Chỉ định trực tiếp Provider hoặc Model
Bạn có thể bỏ qua cơ chế xoay vòng pool và yêu cầu xử lý từ một provider cụ thể bằng HTTP Headers:
```http
X-Provider: gemini:api:01
X-Model: gemini-3.1-pro
```

### 5. Lập trình với Python SDK
```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8787/v1",
    api_key="am-proxy",
)

# Hỗ trợ đầy đủ Server-Sent Events (SSE) Streaming
stream = client.chat.completions.create(
    model="default",
    messages=[{"role": "user", "content": "Viết thuật toán QuickSort trong Go"}],
    stream=True
)
for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
```

---

## 🧰 Luồng Claude Code ↔ Proxy ↔ Tools

Claude Code, Cursor và Antigravity **không** thực thi lệnh trên server amux. Client hoàn toàn sở hữu các công cụ hệ thống cục bộ (`Bash`, `Read`, `Edit`, `Write`...); nhiệm vụ của proxy là chuyển ngữ tương thích giữa các khối `tool_use` (Anthropic), `tool_calls` (OpenAI) hoặc `functionCall` (Gemini) để vòng lặp tác tử hoạt động mượt mà.

### Chuẩn hóa giao tiếp công cụ

| Client | Endpoint Proxy tiếp nhận | Định dạng Tool Wire Format |
| :--- | :--- | :--- |
| **Claude Code** | `POST /v1/messages` | Anthropic `tools[]` + `tool_use` / `tool_result` |
| **Cursor / Codex** | `POST /v1/chat/completions` | OpenAI `tools[].function` + `tool_calls` |
| **Antigravity (AGY)** | `POST /v1beta/models/...` | Gemini `functionDeclarations` / `functionCall` |

Tầng giữa `pkg/tools` chuyển đổi tất cả về cấu trúc canonical `types.ChatRequest`, sau đó các adapter sẽ phát sinh định dạng wire chuẩn tương ứng cho upstream.

### Cơ Chế WebLoop Emulation cho Web Sessions (`webloop.go`)
Đối với các tài khoản Web Session (như ChatGPT Web, Claude Web, Gemini Web) vốn không hỗ trợ gọi function gốc qua API:
- amux đóng gói danh mục công cụ và quy tắc thực thi vào system prompt.
- Phân tích cú pháp phản hồi văn bản của web model để bóc tách các thẻ `<tool_call>`, cú pháp XML hoặc bash markdown fences.
- Chuyển đổi các phát hiện này thành `types.ToolCall` hợp lệ để gửi về client.
- Xử lý các tình huống từ chối hỗ trợ tool hoặc yêu cầu paste mã nguồn để buộc model phát sinh tool call tự động.

---

## 🚀 Antigravity / AGY (Google GenAI)

Antigravity (AGY CLI & IDE) tương tác qua giao thức chuẩn Google Gemini:
- `POST /v1beta/models/...:generateContent` & `:streamGenerateContent`
- `POST /v1beta/models/...:countTokens`
- `GET /v1beta/models`

### Tự Động Khởi Chạy & Hooks
1. **Đăng ký Hooks tự động:**
   Lệnh `am hook install` hoặc `am setup` sẽ cấu hình các file lifecycle hooks:
   - **Antigravity (AGY):** `~/.gemini/config/hooks.json` → gọi `am hook agy start` / `stop`
   - **Claude Code:** `~/.claude/settings.json` → gọi `am hook claude start` / `stop`
   - **Codex:** `~/.codex/hooks.json` → gọi `am hook codex start`
   - **Cursor:** `~/.cursor/hooks.json` → gọi `am hook cursor start`

2. **Cách chạy AGY với proxy amux:**
   ```sh
   # Export biến môi trường vào shell (thiết lập GEMINI_API_BASE & GOOGLE_GENAI_BASE_URL trỏ về proxy)
   eval "$(am env)"
   agy
   ```
   > *Lưu ý: Nếu đã cài đặt hooks (`am hook install` hoặc `am setup`), proxy daemon sẽ tự động khởi động và kết thúc theo vòng đời phiên làm việc của AGY.*

3. **Quản lý Profile AGY:**
   ```sh
   am current antigravity         # Xem email Antigravity đang login từ macOS Keychain
   am add antigravity <tên>       # Lưu profile AGY vào pool amux
   ```

---

## ⚙️ Cấu Hình Provider Pool (`~/.am/accounts.json`)

Mỗi provider được gán một độ ưu tiên (`priority`), số càng nhỏ sẽ được ưu tiên điều hướng trước. Giá trị khóa API có thể liên kết trực tiếp với biến môi trường bằng cú pháp `env:TEN_BIEN`.

Quy chuẩn ID tài khoản: `brand[:method]:NN` (ví dụ: `github:api:01`, `gemini:api:01`, `claude:web:01`). Các ID cũ được hệ thống tự động nhận diện và nâng cấp.

> ⚠️ **Lưu ý:** File `~/.am/accounts.json` trên ổ đĩa được mã hóa tự động bằng thuật toán **AES-256-GCM** (định dạng `AMENC1:`) thông qua khóa chủ trong macOS Keychain. Bạn không nên chỉnh sửa file này thủ công mà hãy dùng các lệnh CLI như `am login`, `am api add`, `am pool priority`, `am pool model`, `am off`, `am on`.

Bạn có thể tham khảo file mẫu chưa mã hóa tại [`accounts.example.json`](accounts.example.json):

```json
{
  "providers": [
    {
      "id": "github:api:01",
      "type": "openai_compatible",
      "priority": 1,
      "baseUrl": "https://models.github.ai/inference",
      "apiKey": "env:GITHUB_MODELS_TOKEN",
      "model": "gpt-4o"
    },
    {
      "id": "gemini:api:01",
      "type": "openai_compatible",
      "priority": 2,
      "baseUrl": "https://generativelanguage.googleapis.com/v1beta/openai",
      "apiKey": "env:GOOGLE_AI_STUDIO_KEY",
      "model": "gemini-3.6-flash"
    },
    {
      "id": "groq:api:01",
      "type": "openai_compatible",
      "priority": 3,
      "baseUrl": "https://api.groq.com/openai/v1",
      "apiKey": "env:GROQ_API_KEY",
      "model": "llama-3.3-70b-versatile"
    },
    {
      "id": "kimi:api:01",
      "type": "openai_compatible",
      "priority": 4,
      "baseUrl": "https://api.moonshot.cn/v1",
      "apiKey": "env:KIMI_API_KEY",
      "model": "moonshot-v1-128k"
    },
    {
      "id": "grok:api:01",
      "type": "openai_compatible",
      "priority": 5,
      "baseUrl": "https://api.x.ai/v1",
      "apiKey": "env:XAI_API_KEY",
      "model": "grok-2-latest"
    },
    {
      "id": "chatgpt:01",
      "type": "chatgpt_web",
      "priority": 5,
      "sessionToken": "env:CHATGPT_SESSION_TOKEN"
    },
    {
      "id": "claude:web:01",
      "type": "claude_web",
      "priority": 6,
      "sessionKey": "env:CLAUDE_SESSION_KEY",
      "model": "claude-3-5-sonnet-20241022"
    }
  ]
}
```

### Mở Rộng Ra Mạng Cục Bộ (`--public`)
Khi cần chia sẻ gateway cho máy khác trong mạng nội bộ:
```sh
am proxy up --public            # Lắng nghe trên 0.0.0.0:8787
am proxy up --public -p 9000    # Đổi sang cổng tuỳ chọn 9000
am proxy token                  # In API key tạm thời (amux-<token>) cấp phát cho client ngoài
eval "$(am env --public)"       # Xuất biến môi trường trỏ trực tiếp đến địa chỉ IP mạng LAN
```

---

## 🏛️ Kiến Trúc Hệ Thống & Tài Liệu Kỹ Thuật

- 📖 Xem chi tiết thiết kế Domain-Driven Design (DDD), phân tầng các package và sơ đồ luồng dữ liệu tại [STRUCT.md](STRUCT.md).

## 📄 Bản Quyền

Dự án được phát hành theo giấy phép [MIT](LICENSE).
