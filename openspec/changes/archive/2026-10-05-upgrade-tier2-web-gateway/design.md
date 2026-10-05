## Context

Tầng Web Accounts (Tier 2: Claude Web, ChatGPT Web, Gemini Web) cho phép AMUX đóng vai trò là AI Gateway miễn phí / chi phí cố định khi tài khoản Subscription cạn hạn mức. Tuy nhiên, do bản chất Web Chat không có API function calling có cấu trúc và context window hạn chế, các vòng lặp Agent (Agent Tool Loops) thường xuyên gặp phải lỗi từ chối (`refusal prose`), tràn context do output log lớn, hoặc gãy chuỗi hội thoại khi thread phình to.

## Goals / Non-Goals

**Goals:**
- Chuẩn hóa cơ chế bọc cấu trúc Tool Call bằng delimiter rõ ràng (`<<<AMUX_TOOL ... >>>`) và Dynamic Few-shot micro-injection.
- Tự động nén và tỉa gọn `tool_result` lớn (> 4KB) trước khi đẩy vào Web backend (Head/Tail log pruning).
- Cải thiện quản lý vòng đời hội thoại per-project, chủ động phòng ngừa context pollution và tràn token.
- Hỗ trợ phát hiện nhanh lỗi auth/timeout từ Web để failover không gây đơ IDE.

**Non-Goals:**
- Thay thế API trả phí chính thức cho các tác vụ tải nặng hàng trăm nghìn token mà không nén.
- Can thiệp vào quyền thực thi lệnh trên máy client (Client vẫn là bên duy nhất thực thi lệnh Bash/Read/Edit).

## Decisions

1. **Structured Enveloping over Heuristic Regex**:
   - *Quyết định*: Sử dụng delimiter `<<<AMUX_TOOL name="..." id="..."> ... <<<END_AMUX_TOOL>>>` và `<tool_call>` có định dạng JSON nghiêm ngặt kèm micro-few-shot mẫu.
   - *Lý do*: Giảm thiểu sự phụ thuộc vào hàng loạt regex phỏng đoán tiếng Anh / tiếng Việt.
   - *Giải pháp thay thế*: Dùng JSON schema lồng sâu trong system prompt (quá nặng đối với web chat, tốn token).

2. **Smart Head/Tail Log Pruner for Web Requests**:
   - *Quyết định*: Tự động rút ngắn các `tool_result` dài hơn 4KB khi chuyển đổi sang prompt gửi lên Web: giữ 30 dòng đầu, 40 dòng cuối và chèn marker tóm tắt ở giữa.
   - *Lý do*: Tiết kiệm đến 70-85% token context cho Web backend mà vẫn giữ trọn vẹn thông tin lệnh bắt đầu và stacktrace/exit code.

3. **Predictive TTFT & Soft Failover**:
   - *Quyết định*: Giám sát Time-to-First-Token của Web requests. Khi gặp lỗi auth hoặc timeout, lập tức đánh dấu cooldown slot Web và chuyển tiếp sang slot tiếp theo hoặc Tier 3.

## Risks / Trade-offs

- *[Risk]*: Log pruner có thể cắt mất dòng thông tin hữu ích ở giữa output log.
  - *Mitigation*: Giữ ngưỡng an toàn 4KB / ~100 dòng, chỉ cắt đối với log lặp lại hoặc quá dài (như build artifacts hoặc raw git log).
- *[Risk]*: Web backend thay đổi cấu trúc trang hoặc chặn IP.
  - *Mitigation*: Tự động bắt mã lỗi 403/429 để chuyển slot hoặc kích hoạt cơ chế failover ngay lập tức.
