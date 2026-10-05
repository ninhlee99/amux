# AMUX (Universal AI Gateway)

<p align="center">
  <img src="https://raw.githubusercontent.com/ninhlee99/amux/main/docs/assets/banner.png" alt="AMUX Banner" width="100%" onerror="this.style.display='none'"/>
</p>

<p align="center">
  <a href="https://github.com/ninhlee99/amux/releases"><img src="https://img.shields.io/github/v/release/ninhlee99/amux?color=blue&label=phi%C3%AAn%20b%E1%BA%A3n" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/gi%E1%BA%A5y%20ph%C3%A9p-MIT-green.svg" alt="License"></a>
  <img src="https://img.shields.io/badge/go-1.22+-00ADD8?logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/h%E1%BB%87%20%C4%91i%E1%BB%81u%20h%C3%A0nh-macOS%20(Apple%20Silicon%20%26%20Intel)-lightgrey?logo=apple" alt="Platform">
  <img src="https://img.shields.io/badge/ki%E1%BB%83m%20th%E1%BB%AD-v%C6%B0%E1%BB%A3t%20qua-brightgreen" alt="Tests">
</p>

<p align="center">
  <b>Universal AI Gateway & Bộ Điều Phối Xoay Tua Tài Khoản Cho Coding Agents.</b><br>
  Quản lý nhiều tài khoản AI — Claude Code, Codex, gói thuê bao Antigravity, phiên web ChatGPT / Gemini / Claude, API keys — trên cùng một máy và chuyển đổi qua lại tức thì không cần đăng nhập lại trình duyệt.
</p>

<p align="center">
  <a href="README.md"><b>English</b></a> • <a href="README_VI.md"><b>Tiếng Việt</b></a>
</p>

---

## ⚡ Điểm Nổi Bật Cốt Lõi

- **Môi Trường Sandbox Cô Lập 100% (`amux run <ide>`)**: Chạy Claude Code, Cursor, Windsurf, Antigravity (AGY), hoặc Codex trong tiến trình con được cô lập trỏ về Gateway. Khi thoát ra, shell và cấu hình hệ thống nguyên vẹn 100%. Các lệnh gọi trực tiếp (`claude`, `cursor`) luôn chạy Native nguyên bản với độ trễ 0ms.
- **Xoay Tua Keychain Ngầm (Silent Keychain Rotation)**: Lưu nhiều tài khoản cho mỗi nhà cung cấp. Khi tài khoản chạm ngưỡng 95% hạn mức, AMUX tự động hoán đổi credential trong Keychain hoặc chuyển tiếp sang tài khoản khả dụng tiếp theo.
- **Web Accounts Làm AI Coding Proxy (WebLoop 2.0)**: Biến phiên Web miễn phí (ChatGPT Web, Gemini Web, Claude Web, Meta Muse) thành backend cho IDE. Động cơ WebLoop hỗ trợ **stream suy nghĩ thời gian thực (`<thought>`)**, tự phục hồi JSON AST hư hỏng và gửi tín hiệu keepalive chống rớt mạng.
- **Bộ Công Cụ MCP Toàn Diện (7 Công Cụ Chuyên Sâu)**: Tích hợp trực tiếp vào mọi IDE hỗ trợ MCP (Cursor, Windsurf, Claude Desktop, VS Code, Zed) với các công cụ lập trình mạnh mẽ: `amux_ask`, `amux_review`, `amux_diagnose`, `amux_fix`, và `amux_analyze`.
- **Bảo Mật Cục Bộ & Không Thu Thập Dữ Liệu (Zero-Telemetry)**: Hoạt động 100% offline trên máy của bạn. Mọi secret lưu tại `~/.amux` được mã hóa AES-256-GCM qua macOS Keychain hoặc file khóa riêng biệt (`~/.amux/master.key`).

---

## 📐 Sơ Đồ Kiến Trúc Hoạt Động

```
                      ┌───────────────────────────────────────┐
                      │              LẬP TRÌNH VIÊN           │
                      └───────────────────┬───────────────────┘
                                          │
                  ┌───────────────────────┴───────────────────────┐
                  ▼ [Chế độ Native: chạy trực tiếp]               ▼ [Chế độ Sandbox: amux run <ide>]
   ┌─────────────────────────────────────────┐     ┌─────────────────────────────────────────┐
   │         NATIVE DIRECT EXECUTION         │     │        AMUX UNIVERSAL AI GATEWAY        │
   │  • Đọc token đang active từ Keychain    │     │  • HTTP Proxy cục bộ tại cổng :8787     │
   │  • Kết nối trực tiếp Upstream (0ms trễ) │     │  • Môi trường tiến trình con cô lập     │
   │  • Không cần gateway chạy ngầm          │     │  • 0% ô nhiễm shell rc & launchctl      │
   └────────────────────┬────────────────────┘     └────────────────────┬────────────────────┘
                        │                                               │
                        │                                               ▼
                        │                          ┌─────────────────────────────────────────┐
                        │                          │        WEBLOOP 2.0 TOOL ENGINE          │
                        │                          │  • Stream Thinking liên tục (SSE)       │
                        │                          │  • Tự sửa lỗi JSON AST hư hỏng          │
                        │                          │  • Phân giải ngoặc [Tool call: ...]     │
                        │                          │  • Tự tính dòng thực tế cho AGY         │
                        │                          └────────────────────┬────────────────────┘
                        │                                               │
                        ▼                                               ▼
   ┌─────────────────────────────────────────────────────────────────────────────────────────┐
   │                              CÁC TẦNG PROVIDERS & MODEL POOL                            │
   │   Tầng 1: Subscriptions       │   Tầng 2: Web Accounts        │   Tầng 3: Metered APIs  │
   │   (Claude, Codex, AGY OAuth)  │   (ChatGPT, Gemini, Claude)   │   (DeepSeek, Groq, Kimi)│
   └─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 📦 Hướng Dẫn Cài Đặt

### Cách 1: Cài đặt nhanh bằng 1 dòng lệnh (Khuyến nghị — Không cần `sudo`)

Tự động tải bản build sẵn hoặc biên dịch từ mã nguồn vào `$HOME/.local/bin/amux` và cấu hình biến môi trường:

```bash
curl -fsSL https://raw.githubusercontent.com/ninhlee99/amux/main/install.sh | sh
```

### Cách 2: Tự biên dịch từ mã nguồn (Go 1.22+)

```bash
git clone https://github.com/ninhlee99/amux.git && cd amux
go build -o amux .

# Cài vào user binaries (Không cần sudo):
mkdir -p ~/.local/bin && cp amux ~/.local/bin/

# Kiểm tra cài đặt:
amux help
```

---

## 🚀 Khởi Động Nhanh Trong 3 Phút

### Bước 1: Kết nối tài khoản AI

```bash
# Đăng nhập gói Subscription chính thức (mở OAuth trình duyệt):
amux login claude        # Đăng nhập Tài khoản 1 cho Claude Code
amux login claude        # Đăng nhập Tài khoản 2 (Tài khoản 1 được lưu an toàn)

# Đăng nhập tài khoản Web (tận dụng quota web):
amux login chatgpt       # Đăng nhập phiên ChatGPT Web
amux login gemini-web    # Đăng nhập phiên Gemini Web

# (Tùy chọn) Thêm API key trả phí:
amux login groq --token gsk_...
```

### Bước 2: Chạy IDE trong môi trường Sandbox an toàn

Chạy công cụ lập trình qua AMUX Gateway mà không làm thay đổi biến môi trường toàn cục của máy:

```bash
amux run claude          # Chạy Claude Code CLI trong sandbox
amux run cursor          # Khởi chạy Cursor IDE trỏ về Gateway :8787
amux run windsurf        # Khởi chạy Windsurf trỏ về Gateway :8787
amux run agy             # Chạy Google Antigravity CLI trong sandbox
amux run codex           # Chạy OpenAI Codex CLI trong sandbox
```

*Khi bạn đóng phiên làm việc, máy tính của bạn sạch sẽ 100%. Nếu gõ trực tiếp `claude` ngoài terminal, Claude Code sẽ chạy Native với tài khoản đang chọn.*

### Bước 3: Giám sát trực quan qua Dashboard TUI

```bash
amux dashboard
```

Giao diện đồ họa terminal tương tác cho phép bạn theo dõi hạn mức quota theo thời gian thực, độ trễ phản hồi, và bật/tắt tài khoản trong pool xoay tua.

---

## 🔄 Chuyển Đổi Tài Khoản & Cơ Chế Xoay Vòng (Pool)

### Chuyển Đổi Thủ Công (Tức thì, không cần login lại)

Chuyển đổi tài khoản sẽ cập nhật token trong Keychain và các file config tương ứng ngay lập tức:

```bash
amux account list          # Xem danh sách tất cả tài khoản
amux switch claude:code:01 # Chuyển Claude Code sang tài khoản 1
amux switch                # Mở menu số tương tác để chọn nhanh
```

```
ID                       EMAIL              TYPE  ACTIVE  POOL  USAGE   THRESHOLD  RESETS IN
claude:code:01           alice@company.com  sub   yes     no    12%     100.0%     4h10m
claude:code:02           alice@gmail.com    sub   -       no    0%      100.0%     unknown
chatgpt:web:01           personal@web       web   -       yes   --      95.0%      ready
```

### Cơ Chế Xoay Tua Tự Động (Rotation Pool)

Pool là tập hợp các tài khoản mà AMUX được phép **tự động chuyển tiếp** khi tài khoản đang chạy bị chạm trần hạn mức:

```bash
amux pool                                    # Xem các tài khoản trong pool
amux pool add claude:code:01 claude:code:02  # Cho phép tự động xoay giữa 2 tài khoản
amux pool remove claude:code:02              # Đưa tài khoản về chế độ thủ công
```

> [!IMPORTANT]
> **Nguyên tắc bảo vệ Subscription:** Tài khoản Subscription (Claude Code, Codex, Antigravity) **KHÔNG BAO GIỜ** tự ý vào pool khi đăng nhập. Chúng chỉ vào pool khi bạn chủ động gõ lệnh `amux pool add`. Tài khoản Web và API key được kích hoạt vào pool mặc định.

---

## 🔌 Tích Hợp Model Context Protocol (MCP)

Biến toàn bộ tài khoản AI của bạn thành công cụ gọi hàm (tools) cho bất kỳ agent nào hỗ trợ MCP (Cursor, Windsurf, Claude Desktop, VS Code, Zed, Cline):

```bash
amux mcp install all       # Tự động phát hiện và cấu hình cho tất cả IDE đã cài
amux mcp status            # Xem trạng thái đăng ký MCP trên hệ thống
amux mcp uninstall         # Gỡ bỏ cấu hình MCP sạch sẽ khỏi các IDE
```

### Bộ 7 Công Cụ MCP Chuyên Nghiệp

| Công Cụ | Chức Năng |
| :--- | :--- |
| `amux_ask` | Hỏi đáp với bất kỳ mô hình hoặc nhóm provider nào (`gemini:web`, `chatgpt`, `claude:code`). |
| `amux_review` | Đánh giá, review mã nguồn chuyên sâu bằng các mô hình lý luận (Reasoning). |
| `amux_diagnose` | Phân tích và chẩn đoán nguyên nhân lỗi biên dịch, exception hoặc log terminal. |
| `amux_fix` | Đề xuất giải pháp và tạo bản vá code chính xác dạng file diff. |
| `amux_analyze` | Đánh giá kiến trúc hệ thống, độ phức tạp thuật toán và hiệu năng. |
| `amux_providers` | Tra cứu danh sách nhà cung cấp khả dụng, trạng thái kết nối và hạn mức. |
| `amux_status` | Kiểm tra kết nối gateway, số phiên hoạt động và trạng thái xoay tua. |

---

## 🛠️ Ma Trận Hỗ Trợ IDE & Coding Agents

| IDE Mục Tiêu | Lệnh Chạy Sandbox | Biến Môi Trường Được Bơm Vào | Ghi Chú |
| :--- | :--- | :--- | :--- |
| **Claude Code** | `amux run claude` | `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY` | Không ô nhiễm Keychain; hoán đổi token OAuth mượt mà. |
| **Cursor IDE** | `amux run cursor` | `OPENAI_BASE_URL`, `OPENAI_API_BASE`, `OPENAI_API_KEY` | Tự động quét từ PATH hoặc `/Applications/Cursor.app`. |
| **Windsurf** | `amux run windsurf` | `OPENAI_BASE_URL`, `OPENAI_API_BASE`, `ANTHROPIC_BASE_URL` | Tự động quét từ PATH hoặc `/Applications/Windsurf.app`. |
| **Antigravity** | `amux run agy` | `GOOGLE_GEMINI_BASE_URL`, `GEMINI_API_BASE`, `GOOGLE_GENAI_BASE_URL` | Tự động nắn chỉnh `AbsolutePath` và tính dòng thực tế. |
| **Codex CLI** | `amux run codex` | `OPENAI_BASE_URL`, `OPENAI_API_BASE`, `OPENAI_API_KEY` | Tự động fallback về direct credentials nếu hết pool. |
| **Bất kỳ Agent nào** | `amux run <cmd>` | Toàn bộ biến môi trường proxy đa giao thức | Hỗ trợ Aider, OpenCode, Cline, v.v. |

---

## 📖 Bảng Tra Cứu Lệnh CLI Đầy Đủ

### 1. Quản Lý Vận Hành & Sandbox
- `amux run <ide> [args...]` — Chạy IDE trong môi trường Sandbox cô lập.
- `amux start` — Khởi động gateway chạy ngầm (`-f` để chạy foreground).
- `amux stop` — Dừng gateway chạy ngầm.
- `amux restart` — Khởi động lại gateway.
- `amux off` — Dừng gateway và đưa toàn bộ IDE về chế độ Native nguyên bản.
- `amux status` — Xem nhanh trạng thái gateway, hook và các tài khoản.

### 2. Quản Lý Tài Khoản & Pool
- `amux login [provider]` — Đăng nhập tài khoản mới (OAuth, Web, API Key).
- `amux account list` (`amux ls`) — Liệt kê tất cả tài khoản và phần trăm đã dùng.
- `amux switch [id]` — Chuyển đổi tài khoản đang active cho công cụ.
- `amux pool [add|remove] <id>` — Quản lý tài khoản trong pool xoay tua tự động.
- `amux account off|on <id>` — Tạm thời tắt hoặc bật lại một tài khoản.
- `amux account remove <id>` — Xóa hoàn toàn một tài khoản khỏi AMUX.
- `amux account health` — Kiểm tra tính hợp lệ của tất cả thông tin đăng nhập.

### 3. Giám Sát & Chẩn Đoán
- `amux dashboard` — Mở dashboard tương tác toàn màn hình trong terminal.
- `amux top` — Xem log thời gian thực các request, token và độ trễ qua gateway.
- `amux doctor [--fix]` — Tự động chẩn đoán quyền hạn, keychain và dọn dẹp config cũ.
- `amux usage [day|week|month]` — Báo cáo thống kê lượng token tiêu thụ.

### 4. Quản Lý MCP Server
- `amux mcp install [target|all]` — Đăng ký AMUX MCP server vào các IDE.
- `amux mcp uninstall [target|all]` — Hủy đăng ký AMUX MCP server khỏi IDE.
- `amux mcp status` — Xem trạng thái đăng ký của MCP server.

---

## 🔒 Mô Hình Bảo Mật & Quyền Riêng Tư

- **Hoàn toàn cục bộ (100% Local)**: AMUX không gửi dữ liệu telemetry hay gọi về bất kỳ máy chủ bên thứ ba nào.
- **Mã hóa thông tin xác thực**: Toàn bộ dữ liệu trong `~/.amux` được mã hóa AES-256-GCM.
  - Mặc định: Khóa chính (master key) được lưu an toàn trong macOS Keychain.
  - Chế độ file: Gõ lệnh `amux config secret-store file` để lưu khóa tại `~/.amux/master.key` mà không cần chạm vào Keychain hệ thống.
- **Ràng buộc Localhost**: Gateway mặc định chỉ lắng nghe tại `127.0.0.1:8787`.

---

## 🧹 Hướng Dẫn Gỡ Bỏ Sạch Sẽ (Uninstall)

AMUX tôn trọng hệ thống của bạn và cho phép gỡ bỏ hoàn toàn không để lại rác:

| Lệnh | Thành phần được gỡ bỏ | Thành phần được giữ lại |
| :--- | :--- | :--- |
| `amux off` | Hủy hook các IDE; dừng gateway | Tài khoản, đăng nhập, MCP |
| `amux mcp uninstall` | Xóa đăng ký AMUX khỏi config của IDE | Các MCP server khác của bạn |
| `amux uninstall` | Dừng gateway, xóa hook, statusline, MCP, nhị phân | Thư mục `~/.amux` (các tài khoản) |
| `amux uninstall --purge` | Toàn bộ thành phần trên + `~/.amux` + Keychain | Hệ thống sạch sẽ 100% như ban đầu |

---

## ❓ Xử Lý Sự Cố Thường Gặp (Troubleshooting)

- **IDE báo lỗi kết nối ngay sau khi gõ `amux stop`**: IDE vẫn còn lưu biến trỏ về gateway. Gõ `amux start` để bật lại hoặc `amux off` để trả về Native.
- **Lệnh `amux switch` báo đăng nhập đã hết hạn**: Gõ `amux login <provider>` một lần để làm mới phiên đăng nhập trên trình duyệt.
- **Cổng 8787 bị chiếm dụng**: Kiểm tra tiến trình xung đột bằng `lsof -i :8787` hoặc đổi cổng với `export AMUX_PORT=8788`.
- **Kiểm tra tự phục hồi**: Gõ `amux doctor --fix` để hệ thống tự động kiểm tra và sửa chữa toàn bộ môi trường.

---

## 📄 Giấy Phép

Phát hành dưới giấy phép mã nguồn mở MIT © [ninhlee99](https://github.com/ninhlee99).
