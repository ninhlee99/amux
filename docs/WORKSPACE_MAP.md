# Workspace Architecture & Client Routing — AMUX

AMUX đóng vai trò là **Universal AI Gateway** chạy ngầm tại cổng `:8787`, hỗ trợ các client AI IDE (Claude Code, Cursor, Codex CLI, Antigravity) kết nối trực tiếp hoặc qua proxy gateway. (Note: ALL old CLIs and legacy proxy architectures have been completely removed. Only `amux` is supported and used.)

---

## 1. Kết Nối Client Vào AMUX Gateway

Các công cụ lập trình AI kết nối với AMUX qua biến môi trường hoặc cấu hình native:

| Client | Cách kết nối | Endpoint Gateway |
|--------|--------------|------------------|
| **Claude Code** | `eval "$(amux env)"` hoặc `amux hook --claude` | `ANTHROPIC_BASE_URL=http://127.0.0.1:8787` (`/v1/messages`) |
| **Cursor IDE** | `amux hook --cursor` hoặc cấu hình Base URL | `http://127.0.0.1:8787/v1` (`/v1/chat/completions`) |
| **Codex CLI** | `amux hook --codex` | `OPENAI_BASE_URL=http://127.0.0.1:8787/v1` (`/v1/responses`) |
| **Antigravity** | `amux hook --agy` | `GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787` (`/v1beta/models/...`) |

---

## 2. Quản Lý Daemon & Hooks

Sử dụng bộ lệnh `amux` mới (đã loại bỏ hoàn toàn các CLI cũ như `am`, `ag`, và kiến trúc cũ, CHỈ SỬ DỤNG DUY NHẤT `amux` làm CLI chính thức):

```bash
# Bật Gateway daemon
amux start

# Bật Gateway foreground để xem log trực tiếp
amux start -f

# Tự động cấu hình hook cho toàn bộ công cụ
amux hook --all

# Kiểm tra dashboard trạng thái runtime
amux status

# Gỡ bỏ hook và trả về kết nối trực tiếp
amux unhook --all

# Dừng Gateway daemon
amux stop
```

---

## 3. Quản Lý Danh Tính (Identities) & Định Tuyến

Mọi thông tin danh tính được lưu trữ tại `~/.amux/identities.json` và quản lý qua CLI:

```bash
# Xem danh sách danh tính và trạng thái
amux account list

# Chọn nhanh tài khoản kích hoạt
amux switch <id>

# Bật/tắt tài khoản tham gia pool định tuyến
amux account on <id>
amux account off <id>

# Chẩn đoán sức khỏe hệ thống
amux doctor
```

> **Quy tắc quan trọng:**
> - Tuyệt đối không bật các account Subscription khi có chỉ định chỉ dùng Web accounts.
> - Các tài khoản Web (Gemini Web, ChatGPT Web) đóng vai trò làm backend mô phỏng API chuẩn cho Claude Code, Antigravity, Codex.

