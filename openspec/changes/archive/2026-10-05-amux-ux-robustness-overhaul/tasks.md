## 1. Webloop 2.0 & Self-Healing Tool Engine

- [x] 1.1 Tạo module AST JSON auto-repair (`pkg/tools/jsonrepair/repair.go`) xử lý unclosed braces, missing quotes, raw newlines, trailing commas
- [x] 1.2 Nâng cấp `pkg/tools/webloop.go` tích hợp bộ sửa lỗi AST và tách biệt thẻ `<thought>` với `<tool_call>`
- [x] 1.3 Cập nhật prompt preamble với chữ ký xác thực phân định chống giả mạo tool call
- [x] 1.4 Thêm cơ chế Sub-second Transparent Retry/Failover khi Web Model trả về văn bản không chứa tool call hợp lệ
- [x] 1.5 Viết unit test & stress test cho bộ JSON repair và multi-turn tool calling trên Web sessions

## 2. Trải Nghiệm TUI Dashboard & Hợp Nhất CLI

- [x] 2.1 Xây dựng màn hình TUI trực quan `amux dashboard` hiển thị realtime: Gateway Status, Active Accounts, Health Latency, 5h/7d Quota Bars
- [x] 2.2 Tích hợp phím điều hướng để Toggle trạng thái Account trong Pool trực tiếp từ TUI
- [x] 2.3 Rút gọn và hợp nhất các lệnh CLI (`amux login`, `amux status`, `amux pool`, `amux doctor`)
- [x] 2.4 Cải tiến lệnh `amux doctor --fix` tự động kiểm tra và dọn dẹp các cấu hình lỗi/xung đột

## 3. Hoàn Thiện & Kiểm Thử E2E 4 IDE Cốt Lõi (Claude Code, Cursor, AGY, Codex)

- [x] 3.1 Bổ sung lệnh `amux run <ide>` hỗ trợ khởi chạy 4 IDE (Claude Code, Cursor, AGY, Codex) với biến môi trường sandbox cô lập
- [x] 3.2 Xây dựng bộ kiểm thử E2E giả lập (Mock Client) chạy chu trình đa bước (Prompt → Tool Call → Tool Result → Completion) cho toàn bộ 4 IDE: Claude Code, Cursor, AGY, Codex

## 4. Quản Lý Định Danh & An Toàn Dữ Liệu

- [x] 4.1 Tối ưu hóa model tài khoản, lưu trữ và tự động giải mã Secret Store
- [x] 4.2 Cập nhật tài liệu hướng dẫn trực quan hóa quy trình sử dụng cho người dùng
