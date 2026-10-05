# Flawless Agent Tool Experience & Web Account Parity

## Why

Đánh giá nghiêm khắc từ góc độ người dùng khó tính và thực tế phát triển phần mềm hàng ngày cho thấy:
Mặc dù amux đã xây dựng được kiến trúc Universal Gateway, Silent Keychain Rotation và bộ chuyển đổi giao thức, nhưng **trải nghiệm thực tế (UX) và tính ổn định khi các IDE Agent (Claude Code, Cursor, AGY, Codex, Windsurf) sử dụng Tools & MCP thông qua các tài khoản Web (Claude Web, ChatGPT Web, Gemini Web)** vẫn còn những hạt sạn và khiếm khuyết nghiêm trọng:

1. **Hiện tượng nghẽn / "treo đơ" cảm giác người dùng khi gọi Web tools (Streaming Latency & Dropped Thinking)**:
   - Trong `wrapWebStream`, toàn bộ stream từ tài khoản web bị buffer 100% đến khi kết thúc mới trả về. Trong thời gian này (20-60 giây), IDE không nhận được bất kỳ token nào. Đặc biệt, các chunk `Thinking` (tư duy suy luận) của mô hình bị nuốt chửng hoàn toàn (`buf.WriteString(ch.Content)` bỏ qua `ch.Thinking`). Người dùng nhìn màn hình đóng băng không biết agent đang chạy hay đã chết kết nối.
2. **Lệch pha định dạng lịch sử hội thoại (History Formatting Disconnect)**:
   - Trong `BuildConcatenatedPrompt`, các lượt tool call trước đó của assistant được format dạng `[Tool call: TOOL id=...]`, trong khi preamble hướng dẫn mô hình phát sinh `<tool_call>`.
   - Các regex trong `webloop.go` (`reBracketTool`, `reBracketToolAlt`) không bao quát được biến thể `[Tool call: Name ...]`. Khi mô hình Web bắt chước lại lịch sử của chính mình ở turn 2, AMUX không nhận diện được đó là tool call mà coi là plain text, khiến agent coding đứt gãy luồng thực thi giữa chừng.
3. **Lỗi đường dẫn tương đối làm gãy các Tool khắt khe (AGY Absolute Path Rejection)**:
   - Các mô hình Web luôn trả về đường dẫn tương đối (như `main.go`, `pkg/cli/run.go`). Các công cụ khắt khe của Antigravity/AGY (`view_file`, `replace_file_content`, `write_to_file`) đòi hỏi `AbsolutePath` / `TargetFile` phải là đường dẫn tuyệt đối. AMUX chỉ đổi tên thuộc tính mà không resolve sang `filepath.Abs`, dẫn đến lỗi validation từ chối thực thi liên tục.
4. **Hardcode `EndLine: 1000000` phá hỏng các tác vụ sửa code (`replace_file_content`)**:
   - Khi mô hình không phát sinh `EndLine`, AMUX tự điền `1000000`. Validator của AGY kiểm tra nghiêm ngặt `StartLine <= EndLine <= số dòng của file`. Với các file nhỏ hơn 1 triệu dòng, AGY văng lỗi không thể sửa file. Cần đọc file trên đĩa để tính đúng `StartLine` và `EndLine` hoặc giới hạn theo số dòng thực tế.
5. **Cắt tỉa kết quả tool (Tool Result Pruning) làm mất thông tin lỗi ở đuôi**:
   - Khi cắt bớt kết quả tool cũ hơn 2000 ký tự, AMUX chỉ lấy 2000 ký tự đầu (`toolContent[:2000]`). Nhưng hầu hết các thông báo lỗi (panic, failed assertions, stack traces) lại nằm ở cuối! Cần bảo toàn cả Head (1000 ký tự) và Tail (1000 ký tự).
6. **Môi trường Sandbox thiếu biến môi trường cho Gemini / Antigravity**:
   - `PrepareSandboxEnv` cho `agy` chỉ set `GOOGLE_GEMINI_BASE_URL`, bỏ sót `GEMINI_API_BASE` và `GOOGLE_GENAI_BASE_URL` vốn được các phiên bản Google GenAI SDK sử dụng.
7. **Phát hiện thiếu ứng dụng macOS trong `amux doctor`**:
   - `hook.CursorAvailable()` và `WindsurfAvailable` chỉ tìm binary trên PATH hoặc thư mục config ẩn, bỏ sót các ứng dụng cài trong `/Applications/*.app`.
8. **Thiếu sót thông tin hướng dẫn trong MCP Server (`amux mcp`)**:
   - Trợ giúp chỉ liệt kê 3 công cụ trong khi amux có 7 công cụ đầy đủ (`amux_review`, `amux_diagnose`, `amux_fix`, `amux_analyze`, `amux_providers`, `amux_ask`, `amux_status`).

## What Changes

- **WebLoop 2.0 Streaming Thinking & Keepalive**:
  - Stream ngay lập tức các chunk `Thinking` từ web backend tới client IDE trong khi đang gom và thẩm định tool call.
  - Trích xuất các khối `<thought>...</thought>` phát sinh trong text để truyền thành thinking block chuẩn cho Claude Code, Cursor, AGY.
- **Chuẩn hóa & Đồng bộ Cú pháp Lịch sử Tool**:
  - `BuildConcatenatedPrompt` phát sinh cú pháp `<tool_call>\n{"name":"...", "arguments":...}\n</tool_call>` đồng nhất 100% với system preamble.
  - Mở rộng regex `reBracketTool` thành `(?is)\[(?:tool[ _]call|tool_call):?\s+(?:name="?)?([A-Za-z0-9_-]+)"?(?:\s+id="?([^"\s\]]+)"?)?\]\s*(\{[\s\S]*?\})` để bắt trọn mọi biến thể bracket do ChatGPT/Claude Web sinh ra.
- **Tự động chuyển đổi đường dẫn tương đối thành tuyệt đối**:
  - Tự động convert đường dẫn tương đối sang `filepath.Abs` khi client yêu cầu `AbsolutePath` hoặc `TargetFile`.
- **Tính toán Thông minh `StartLine` & `EndLine` từ File thực tế**:
  - Kiểm tra file trên đĩa để tìm chính xác dòng xuất hiện của `TargetContent`, giới hạn `EndLine` theo tổng số dòng của file, loại bỏ hoàn toàn con số hardcode 1,000,000.
- **Bảo toàn Cả Đầu và Đuôi khi Prune Tool Results**:
  - Giữ 1000 ký tự đầu và 1000 ký tự cuối để giữ trọn vẹn lỗi / exit codes của CLI.
- **Bổ sung Đầy đủ Biến Môi trường Gateway cho Sandbox**:
  - Cung cấp `GOOGLE_GEMINI_BASE_URL`, `GEMINI_API_BASE`, `GOOGLE_GENAI_BASE_URL`, `GEMINI_API_KEY` cho `agy` và arbitrary command runners.
- **Nâng cấp Hệ thống Nhận diện IDE trên macOS**:
  - Kiểm tra `/Applications/Cursor.app`, `/Applications/Antigravity.app`, `/Applications/Windsurf.app` trong `amux doctor` và hook.
- **Hoàn thiện Bộ tài liệu & Hướng dẫn MCP Server**:
  - Cập nhật đầy đủ 7 công cụ và prompt instructions chi tiết cho `amux mcp`.

## Capabilities

### Modified Capabilities
- `webloop-self-healing`: Nâng cấp streaming thinking deltas, bắt chuẩn cú pháp bracket, và tự động sửa đường dẫn/dòng file.
- `web-tool-optimization`: Chuẩn hóa prompt format giữa assistant tool history và preamble, bảo toàn head+tail khi prune.
- `ide-integration`: Hoàn thiện biến môi trường cho mọi IDE coding agent và nhận diện app bundle macOS.
- `mcp-server`: Cập nhật toàn diện tài liệu và danh mục 7 công cụ trợ lý phát triển phần mềm.

## Impact

- **Affected packages**: `pkg/tools`, `pkg/provider`, `pkg/cli`, `pkg/hook`, `pkg/mcp`, `pkg/bridge`.
- **User-facing changes**: Không còn độ trễ đơ lag khi mô hình đang suy luận; agent coding không còn bị gãy tool loop ở turn 2; các lệnh chỉnh sửa file trên AGY hoạt động mượt mà 100%; `amux doctor` hiển thị chính xác các IDE đã cài đặt.
