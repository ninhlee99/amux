# Tasks: Flawless Agent Tool Experience & Web Account Parity

## 1. WebLoop 2.0 Streaming Thinking & Keepalive
- [x] 1.1 Stream ngay lập tức các chunk `Thinking` trong `wrapWebStream` của `pkg/tools/webloop.go` <!-- id: 1.1 -->
- [x] 1.2 Trích xuất khối `<thought>` text thành thinking stream chunk nếu mô hình xuất dạng thẻ XML <!-- id: 1.2 -->
- [x] 1.3 Bổ sung test kiểm thử `TestWrapWebStream_StreamsThinkingImmediately` trong `pkg/tools/webloop_thought_test.go` <!-- id: 1.3 -->

## 2. Đồng bộ Cú pháp Lịch sử Tool & Regex Lenient Parsing
- [x] 2.1 Chuẩn hóa `BuildConcatenatedPrompt` trong `pkg/provider/chatgpt_web.go` để assistant tool calls xuất dạng `<tool_call>` <!-- id: 2.1 -->
- [x] 2.2 Cập nhật định dạng tool result thành `[Tool result (NAME)]:` và prune giữ cả head+tail <!-- id: 2.2 -->
- [x] 2.3 Mở rộng regex `reBracketTool` trong `pkg/tools/webloop.go` để bắt mọi biến thể `[Tool call: ...]` <!-- id: 2.3 -->
- [x] 2.4 Bổ sung unit test kiểm thử parsing đa dạng bracket và đa tool call <!-- id: 2.4 -->

## 3. Tự động Chuyển đổi Đường dẫn & Tính Dòng Chính xác cho AGY
- [x] 3.1 Thêm logic chuyển đổi đường dẫn tương đối sang tuyệt đối (`filepath.Abs`) trong `pkg/tools/gateway.go` và `pkg/tools/webloop.go` <!-- id: 3.1 -->
- [x] 3.2 Tự động kiểm tra file trên đĩa để xác định chính xác `StartLine` và `EndLine` dựa trên `TargetContent` hoặc tổng số dòng <!-- id: 3.2 -->
- [x] 3.3 Loại bỏ con số hardcode `1000000` gây crash validator AGY <!-- id: 3.3 -->
- [x] 3.4 Bổ sung unit test kiểm thử auto-abs path và bound line calculation <!-- id: 3.4 -->

## 4. Hoàn thiện Sandbox Environment & Nhận diện Ứng dụng
- [x] 4.1 Thêm `GEMINI_API_BASE` và `GOOGLE_GENAI_BASE_URL` vào `PrepareSandboxEnv` trong `pkg/cli/run.go` <!-- id: 4.1 -->
- [x] 4.2 Cập nhật `pkg/hook/hook.go` phát hiện các app bundle `/Applications/*.app` (Cursor, AGY, Windsurf) <!-- id: 4.2 -->
- [x] 4.3 Cập nhật `pkg/cli/doctor.go` dùng hàm phát hiện mới <!-- id: 4.3 -->

## 5. Hoàn thiện Tài liệu & Công cụ MCP Server
- [x] 5.1 Cập nhật `mcpInstructions` và `helpMCP()` trong `pkg/cli/mcp.go` liệt kê đầy đủ 7 công cụ <!-- id: 5.1 -->
- [x] 5.2 Kiểm tra và đồng bộ target MCP cho Antigravity CLI <!-- id: 5.2 -->

## 6. Kiểm tra & Thẩm định Toàn diện (Validation & Regression)
- [x] 6.1 Chạy toàn bộ unit test `go test ./... -count=1` <!-- id: 6.1 -->
- [x] 6.2 Chạy `openspec validate --all --strict --no-interactive` <!-- id: 6.2 -->
