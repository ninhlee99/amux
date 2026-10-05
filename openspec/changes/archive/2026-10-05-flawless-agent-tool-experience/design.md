# Design: Flawless Agent Tool Experience & Web Account Parity

## 1. WebLoop 2.0 Streaming Thinking & Keepalive Architecture

### Vấn đề hiện tại
Khi client gửi yêu cầu có kèm `tools[]`, `wrapWebStream` chặn các chunk `ch.Content` trong bộ đệm nội bộ để quét cú pháp `<tool_call>` hoặc JSON markup. Tuy nhiên:
1. `ch.Thinking` (đến từ Claude Web, Gemini Web, hoặc ChatGPT o-series) hoàn toàn không xung đột với cú pháp tool call. Nhưng lại bị nuốt chửng do `wrapWebStream` chỉ ghi `buf.WriteString(ch.Content)`.
2. Client IDE (Claude Code, Cursor, AGY) có các chỉ báo thinking spinner / thinking delta. Do không nhận được chunk nào trong suốt 30-60 giây, IDE coi kết nối như bị treo.

### Giải pháp thiết kế
- Trong vòng lặp đọc `inner`:
  ```go
  if ch.Thinking != "" {
      out <- types.StreamChunk{ID: id, Thinking: ch.Thinking}
  }
  ```
- Khi hoàn tất luồng, quét thêm các khối `<thought>...</thought>` phát sinh trong text tích lũy (nếu có) và phát ra dưới dạng thinking chunk nếu chưa từng phát ra thinking chunk tương tự trước đó.
- Đối với các client hỗ trợ keep-alive comment (như Anthropic SSE, Gemini SSE), duy trì ping định kỳ để đảm bảo socket không bị router hoặc firewall đóng ngang.

## 2. Đồng bộ Cú pháp Lịch sử Tool &AST Lenient Parsing

### Vấn đề hiện tại
Trong `BuildConcatenatedPrompt`:
```go
case "assistant":
    sb.WriteString("Assistant: ")
    sb.WriteString(m.Content)
    for _, tc := range m.ToolCalls {
        sb.WriteString("\n[Tool call: ")
        ...
```
Trong khi đó, preamble chỉ dẫn:
```
<tool_call>
{"name":"TOOL","arguments":{...}}
</tool_call>
```
Sự không đồng nhất này khiến mô hình ở lượt 2 bắt chước cú pháp `[Tool call: ...]`, vượt ngoài tầm kiểm soát của regex cũ.

### Giải pháp thiết kế
1. Chuẩn hóa `BuildConcatenatedPrompt`:
   - Ghi lại các lệnh tool của assistant dưới dạng chuẩn:
     ```
     Assistant: 
     <tool_call>
     {"name":"...", "arguments":{...}}
     </tool_call>
     ```
   - Ghi kết quả tool:
     ```
     [Tool result (TOOL_NAME)]:
     ...
     ```
2. Mở rộng bộ phân giải regex trong `webloop.go`:
   - `reBracketTool` được cập nhật thành:
     `(?is)\[(?:tool[ _]call|tool_call):?\s+(?:name="?)?([A-Za-z0-9_-]+)"?(?:\s+id="?([^"\s\]]+)"?)?\]\s*(\{[\s\S]*?\})`
   - Bắt trọn cả các trường hợp mô hình sinh ra `[Tool call: Bash id=1] {...}` hoặc `[tool_call: Bash] {...}`.

## 3. Tự động chuyển đổi đường dẫn & Bound Line Numbers cho AGY / Strict Tools

### Vấn đề hiện tại
1. Web model không biết thư mục gốc (workspace) của developer trên máy tính cá nhân, nên luôn sinh đường dẫn tương đối (ví dụ: `pkg/tools/gateway.go`).
2. Validator của AGY từ chối bất kỳ đường dẫn nào không phải là absolute path (`filepath.IsAbs == false`).
3. Khi mô hình không phát sinh `EndLine` cho thao tác sửa code (`replace_file_content`), amux tự gán `EndLine = 1000000`, dẫn đến lỗi validation nghiêm ngặt của AGY (`EndLine must satisfy StartLine <= EndLine <= number of lines in the file`).

### Giải pháp thiết kế
Trong `pkg/tools/gateway.go` và `pkg/tools/webloop.go`:
1. Nếu schema yêu cầu `AbsolutePath` hoặc `TargetFile`:
   ```go
   if p, ok := m[prop].(string); ok && p != "" && !filepath.IsAbs(p) {
       if abs, err := filepath.Abs(p); err == nil {
           m[prop] = abs
       }
   }
   ```
2. Đọc file trên đĩa (nếu tồn tại) để xác định tổng số dòng `fileLines`:
   - Nếu có `TargetContent`, tìm chính xác vị trí dòng bắt đầu `foundStart` và dòng kết thúc `foundEnd` của nội dung cần thay thế.
   - Gán `StartLine = foundStart` và `EndLine = foundEnd`.
   - Nếu không tìm thấy hoặc là thao tác khác, giới hạn `EndLine` không vượt quá `fileLines`.

## 4. Bảo toàn Ngữ cảnh Lỗi khi Prune Tool Results

- Khi kết quả tool vượt quá 2000 ký tự ở các turn cũ trong lịch sử:
  - Giữ 1000 ký tự đầu (`head`) và 1000 ký tự cuối (`tail`).
  - Chèn thông báo ngắn gọn ở giữa: `...[older output truncated to preserve prompt limit]...`.
  - Đảm bảo các thông báo lỗi, exit codes, hoặc stack trace ở cuối kết quả CLI không bao giờ bị cắt mất.

## 5. Môi trường Thực thi & Tương thích Hệ điều hành

1. `pkg/cli/run.go`:
   - Bổ sung `GEMINI_API_BASE`, `GOOGLE_GENAI_BASE_URL` cho target `agy` và target tổng quát.
2. `pkg/hook/hook.go`:
   - Bổ sung phát hiện ứng dụng macOS trong `/Applications/Cursor.app`, `/Applications/Antigravity.app`, `/Applications/Windsurf.app`.
3. `pkg/cli/mcp.go`:
   - Cập nhật danh sách đầy đủ 7 công cụ MCP và mô tả chi tiết công dụng.
