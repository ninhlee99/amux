## Why

AMUX hiện tại có nền tảng kỹ thuật tốt nhưng trải nghiệm người dùng (UX) còn quá phân mảnh, phức tạp và thiếu tính ổn định khi chạy thực tế (đặc biệt là Web-based Tool Calling, độ trễ và tính tương thích chéo giữa các IDE). Người dùng phải quản lý quá nhiều khái niệm cấu hình chồng chéo và thường xuyên đối mặt với sự cố rớt tool call, vỡ JSON schema, hoặc lỗi timeout session.

Kế hoạch này đại tu toàn diện 4 trụ cột cốt lõi:
1. **Trải nghiệm Zero-Config & Hợp nhất CLI** (TUI Dashboard trực quan, gom gọn lệnh).
2. **Webloop 2.0 & Robust Self-Healing Tool Engine** (AST auto-repair JSON, two-phase prompting, fallback mượt mà).
3. **Mở rộng & Ổn định hóa E2E cho 100% IDE Coding Agents** (Claude Code, Cursor, AGY, Codex, Windsurf, Cline).
4. **Bảo mật, Cách ly Môi trường & Minh bạch Thông tin Thời gian thực**.

## What Changes

- **Hợp nhất mô hình tài khoản & CLI**: Gộp `identities.json` và `accounts.json` thành cấu trúc `Account` trực quan, tối giản hóa CLI về bộ lệnh tiêu chuẩn `login`, `status`, `dashboard`, `pool`, `doctor`.
- **TUI Live Dashboard**: Bổ sung `amux dashboard` (Terminal User Interface) cho phép theo dõi trực quan trạng thái Gateway, pool health, quota 5h/7d, và toggle account bằng phím mũi tên.
- **Webloop 2.0 Self-Healing Tool Engine**: Bổ sung bộ phân tích AST JSON lenient/repairer, two-phase tool schema injection, và cơ chế chuyển đổi dự phòng ngầm (transparent sub-second retry/failover) khi Web model trả về dữ liệu lỗi.
- **E2E Compatibility Layer cho Mọi IDE**: Chuẩn hóa wire-protocols cho Windsurf, Cline/Roo Code, Cursor, Claude Code, AGY; bổ sung test harness E2E mô phỏng multi-turn tool calling.
- **Sanboxed Environment & Safe Hooking**: Loại bỏ các can thiệp vĩnh viễn gây rủi ro vào hệ thống; chuyển sang cơ chế wrapper an toàn và hook có kiểm soát tuyệt đối.

## Capabilities

### New Capabilities
- `tui-dashboard`: Giao diện dòng lệnh tương tác trực quan (TUI) hiển thị real-time trạng thái gateway, tài khoản, quota, và điều khiển nhanh.
- `webloop-self-healing`: Động cơ phục hồi lỗi cú pháp JSON và tái thực thi tự động (self-repair AST & retry) cho Web-based tool calls.

### Modified Capabilities
- `web-tool-optimization`: Nâng cấp giao thức prompt injection và tách biệt 2 pha (Reasoning vs Tool Call Execution).
- `ide-integration`: Mở rộng hỗ trợ đầy đủ các client Windsurf, Cline/Roo Code, Cursor, Claude Code, Antigravity.
- `account-identity`: Đơn giản hóa cấu trúc định danh và hợp nhất thao tác quản lý tài khoản.

## Impact

- **Affected packages**: `pkg/cli`, `pkg/ui`, `pkg/tools`, `pkg/bridge`, `pkg/provider`, `pkg/identity`, `pkg/gateway`, `pkg/runtime`.
- **User-facing changes**: Giao diện dòng lệnh thân thiện hơn, ít lệnh rườm rà hơn, độ tin cậy của các phiên agentic tăng vọt, không còn hiện tượng treo IDE khi dùng Web account.
