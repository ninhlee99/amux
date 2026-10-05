## 1. Web Tool Prompting & Structured Delimiters

- [x] 1.1 Tối ưu hóa preamble và catalog injection trong `pkg/tools/webloop.go` với micro few-shot 1-shot example
- [x] 1.2 Hoàn thiện bóc tách delimiter `<<<AMUX_TOOL>>>` và XML `<tool_call>` đa định dạng
- [x] 1.3 Cập nhật unit tests cho micro few-shot và bóc tách delimiters trong `pkg/tools/webloop_test.go`

## 2. Smart Tool Result Pruner & Context Optimization

- [x] 2.1 Bổ sung hàm rút gọn kết quả công cụ lớn (`PruneToolResult` / Head/Tail log pruner) trong `pkg/tools/webloop.go`
- [x] 2.2 Tích hợp pruner vào luồng format multi-turn prompt của Web Adapters trong `pkg/provider/prompt.go`
- [x] 2.3 Viết unit test kiểm chứng log pruning bảo toàn header và tail error lines

## 3. Web Conversation Hygiene & Verification

- [x] 3.1 Cập nhật `pkg/provider/project_conv.go` để xử lý dọn dẹp state và luân chuyển thread theo ngưỡng token
- [x] 3.2 Chạy toàn bộ test suite `go test ./...` và kiểm chứng các IDE Agent fixtures
