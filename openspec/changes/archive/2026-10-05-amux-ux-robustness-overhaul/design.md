## Context

AMUX đóng vai trò là Universal AI Gateway & Development Runtime kết nối các IDE Agentic (Claude Code, Cursor, Codex, Antigravity, Windsurf, Cline) với đa tầng Provider (Subscription OAuth, Web CDP Sessions, Metered APIs). Tuy nhiên, sau các phiên đánh giá thực tế từ góc nhìn người dùng khó tính, hệ thống bộc lộ 4 điểm nghẽn lớn:
1. **Quản lý cấu hình rời rạc**: Tách biệt `identities.json` và `accounts.json` gây lúng túng khi debug và cấu hình.
2. **Web Tool Calling không tự phục hồi**: Regex bóc tách tool truyền thống dễ vỡ khi Web Model sinh ra JSON lỗi, thiếu dấu ngoặc, hoặc chèn text giải thích.
3. **Thiếu hỗ trợ toàn diện & kiểm thử E2E chéo IDE**: Một số IDE hiện đại (Windsurf, Roo/Cline) gặp vấn đề với luồng SSE hoặc thiếu header tương thích.
4. **Mất an tâm về tác động hệ thống**: Cơ chế can thiệp toàn cục vào shell env cần chuyển sang dạng sandbox minh bạch.

## Goals / Non-Goals

**Goals:**
- Cung cấp `amux dashboard` (TUI tương tác) hiển thị trực quan sức khỏe Gateway, Pool accounts, Quota 5h/7d và Toggle tức thì.
- Xây dựng **Webloop 2.0 Engine** tích hợp AST JSON Auto-Repair và Two-Phase Prompting để đạt tỷ lệ gọi tool thành công >99% trên Web accounts.
- Thêm cơ chế **Transparent Failover**: Khi Web account gọi tool thất bại, Gateway tự động chuyển tiếp ngầm sang account khác trong pool mà không làm gián đoạn SSE stream về phía IDE.
- Chuẩn hóa E2E Dialect adapter cho toàn bộ: Claude Code, Cursor, Codex, AGY, Windsurf, Cline/Roo Code.
- Tinh giản bộ lệnh CLI về 5 lệnh chuẩn, loại bỏ các cờ và file trung gian gây rối rắm.

**Non-Goals:**
- Tự phát triển một IDE riêng (AMUX giữ nguyên triết lý Zero-Touch Native Mode, tương thích trực tiếp với IDE chính thức).
- Lưu trữ/Log nội dung code của người dùng lên cloud (100% dữ liệu xử lý cục bộ tại máy dev).

## Decisions

### Decision 1: Hợp nhất Account & Identity Domain Model
- **Lý do**: Loại bỏ sự phân vân giữa "tài khoản" và "danh tính". Gom toàn bộ về struct `Account` với trạng thái `Source` (OAuth/Web/API), `PoolStatus` (Active/Idle/Quarantine), và `QuotaMetrics` rõ ràng.
- **Giải pháp thay thế đã xem xét**: Giữ 2 file và tạo symlink -> Bị loại bỏ vì vẫn gây khó hiểu cho người dùng khi backup/migrate.

### Decision 2: Webloop 2.0 với AST Lenient JSON Parser & Repairer
- **Lý do**: Các Web Chat models (Gemini Web, ChatGPT Web) không hỗ trợ Constrained Decoding nên thường xuyên xuất ra trailing commas, single quotes, unescaped newlines, hoặc thiếu dấu đóng `}`.
- **Giải pháp**: Xây dựng package `pkg/tools/jsonrepair` quét token stream, tự động bù ngoặc, sửa dấu nháy, escape ký tự điều khiển trước khi marshal sang JSON chuẩn.

### Decision 3: Two-Phase Tool Prompting & Structured Thinking Separation
- **Lý do**: Khi model vừa suy luận vừa gọi tool trong cùng 1 khối text, việc bóc tách rất dễ nhầm lẫn giữa đoạn code mẫu và lệnh gọi tool thật.
- **Giải pháp**: Quy định rõ block `<thought>...</thought>` và `<tool_call>...</tool_call>` với chữ ký phân định chống giả mạo (`AMUX_EXEC_HASH`).

### Decision 4: Safe Sandboxed Environment & Diagnostic Doctor 2.0
- **Lý do**: Người dùng lo ngại `amux hook` làm bẩn cấu hình `.zshrc` hoặc `launchctl`.
- **Giải pháp**: `amux doctor --fix` tự động kiểm tra tính toàn vẹn của các biến môi trường, dọn dẹp các hook rác, và cung cấp chế độ `amux run <ide>` chạy trong môi trường subshell cô lập (isolated env wrapper).

## Risks / Trade-offs

- **[Risk: Web API thay đổi đột ngột]** → **Mitigation**: Tự động phát hiện lỗi thay đổi cấu trúc web và kích hoạt Circuit Breaker, chuyển mượt về API hoặc thông báo người dùng cập nhật cookie qua CDP.
- **[Risk: Auto-repair JSON làm sai lệch ý định của Model]** → **Mitigation**: Chỉ sửa cú pháp JSON (structural repair), không suy diễn hay thay đổi giá trị của các tham số.
- **[Risk: TUI Dashboard chiếm tài nguyên terminal]** → **Mitigation**: Dùng lightweight terminal drawing (ANSI escapes / Bubbletea minimal) không gây giật lag.
