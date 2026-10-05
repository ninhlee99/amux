## Why

Tài khoản Web (Tier 2: Claude Web, ChatGPT Web, Gemini Web) đóng vai trò là tầng dự phòng chi phí thấp (flat-rate/free-tier) khi tài khoản Subscription cạn hạn mức. Tuy nhiên, việc mô phỏng Function Calling và duy trì vòng lặp Agent (Tool Loop) qua Web Chat hiện tại gặp phải nhiều điểm nghẽn:
1. Web model dễ xuất prose từ chối hoặc sai định dạng thay vì xuất cấu trúc tool_call chuẩn.
2. Output tool lớn (npm test, git diff, file logs) làm phình to context window hạn chế của Web backend, gây lỗi 413 hoặc token truncation.
3. Thread bị nhiễm ngữ cảnh qua nhiều turns dẫn đến ảo giác (hallucination).
4. Web session cookie dễ bị thu hồi gây gián đoạn giữa chừng.

Việc nâng cấp toàn diện Tier 2 sẽ nâng cao độ tin cậy từ ~65% lên >90%, giúp các IDE Agent (Claude Code, Cursor, Antigravity, Codex, Roo Code) tiếp tục lập trình mượt mà ngay cả khi chạy trên Tầng Web.

## What Changes

- **Enhanced Structured Tool Prompting**: Áp dụng thẻ phân tách chuẩn xác cao (`<<<AMUX_TOOL ... >>>`), Dynamic Few-shot micro-injection trước user prompt, và hỗ trợ Fast 1-Turn Self-Correction khi model trả về refusal prose.
- **Smart Tool Result Pruning**: Tự động rút gọn các `tool_result` vượt quá ngưỡng dung lượng (> 4KB), bảo toàn 30 dòng đầu và 40 dòng cuối cùng kèm tóm tắt cấu trúc để tránh tràn context web.
- **Thread Lifecycle & State Sanitization**: Tự động dọn dẹp và làm mới trạng thái hội thoại sau các chuỗi task lớn, tránh context pollution.
- **Autonomous Session Health Check & Failover**: Tự động phát hiện phiên hết hạn / Cloudflare block và failover nhanh sang slot Web kế tiếp hoặc Tier 3 API mà không gây đơ IDE (Time-to-First-Token timeout < 7s).

## Capabilities

### New Capabilities
- `web-tool-optimization`: Tối ưu hóa quá trình tạo prompt, bóc tách cấu trúc công cụ, và nén kết quả thực thi công cụ cho các web provider text-only.

### Modified Capabilities
- `web-providers`: Cải tiến cơ chế quản lý vòng đời hội thoại per-project, quản lý cookie và phản hồi lỗi xác thực nhanh.
- `tool-dialects`: Cải tiến cơ chế bóc tách tool-call và xử lý phản hồi từ chối / phục hồi cấu trúc cho web sessions.

## Impact

- `pkg/tools/webloop.go`: Cải tiến preamble, few-shot injection, bóc tách `<<<AMUX_TOOL>>>`, và bộ lọc smart tool result pruner.
- `pkg/provider/project_conv.go`: Tinh chỉnh cơ chế luân chuyển thread và token tracking.
- `pkg/provider/claude_web.go`, `pkg/provider/chatgpt_web.go`, `pkg/provider/gemini_web.go`: Tối ưu hóa xử lý lỗi, timeout và retry.
- `pkg/router/`: Điều phối failover nhanh khi phát hiện tín hiệu lỗi từ Web backend.
