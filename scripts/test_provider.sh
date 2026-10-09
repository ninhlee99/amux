#!/bin/bash
# ==============================================================================
# AMUX Isolated Provider Test Script
# Usage:
#   ./scripts/test_provider.sh [claude | gemini | chatgpt | <provider_id>] [--interactive]
#
# Examples:
#   ./scripts/test_provider.sh claude              # Chạy benchmark tự động với Claude Web
#   ./scripts/test_provider.sh gemini              # Chạy benchmark tự động với Gemini Web
#   ./scripts/test_provider.sh chatgpt             # Chạy benchmark tự động với ChatGPT Web
#   ./scripts/test_provider.sh claude -i           # Mở Claude Code tương tác qua Claude Web
# ==============================================================================

set -e

PROVIDER_ARG="${1:-}"
MODE="${2:-}"

if [ "$PROVIDER_ARG" = "-i" ] || [ "$PROVIDER_ARG" = "--interactive" ]; then
    MODE="-i"
    PROVIDER_ARG=""
fi

# 1. Ánh xạ provider shorthand sang ID tài khoản cụ thể
TARGET_ID=""
case "$PROVIDER_ARG" in
    claude|claude-web|claude_web)
        TARGET_ID="${TEST_CLAUDE_ACCOUNT:-claude:web:bi117ute}"
        ;;
    gemini|gemini-web|gemini_web)
        TARGET_ID="${TEST_GEMINI_ACCOUNT:-gemini:web:bi117ute}"
        ;;
    chatgpt|chatgpt-web|chatgpt_web)
        TARGET_ID="${TEST_CHATGPT_ACCOUNT:-chatgpt:ninhle21199}"
        ;;
    "")
        echo "=== CHỌN PROVIDER ĐỂ TEST ==="
        echo "1) claude   -> Claude Web  (claude:web:bi117ute)"
        echo "2) gemini   -> Gemini Web  (gemini:web:bi117ute)"
        echo "3) chatgpt  -> ChatGPT Web (chatgpt:ninhle21199)"
        echo ""
        read -p "Nhập lựa chọn (1/2/3 hoặc tên provider): " CHOICE
        case "$CHOICE" in
            1|claude|claude-web)
                TARGET_ID="${TEST_CLAUDE_ACCOUNT:-claude:web:bi117ute}"
                ;;
            2|gemini|gemini-web)
                TARGET_ID="${TEST_GEMINI_ACCOUNT:-gemini:web:bi117ute}"
                ;;
            3|chatgpt|chatgpt-web)
                TARGET_ID="${TEST_CHATGPT_ACCOUNT:-chatgpt:ninhle21199}"
                ;;
            *)
                TARGET_ID="$CHOICE"
                ;;
        esac
        ;;
    *)
        TARGET_ID="$PROVIDER_ARG"
        ;;
esac

if [ -z "$TARGET_ID" ]; then
    echo "Lỗi: Chưa chọn provider hợp lệ."
    exit 1
fi

TEST_PORT="18789"
TEST_DIR="/tmp/amux-test-sandbox"

echo "================================================="
echo "  AMUX Isolated Provider Test"
echo "  Target Provider: $TARGET_ID"
echo "  Test Port:       $TEST_PORT (Cổng 8787 được giữ nguyên)"
echo "  Sandbox Dir:     $TEST_DIR"
echo "================================================="

# Dọn dẹp tiến trình cũ trên cổng test nếu có
lsof -ti :"$TEST_PORT" | xargs kill -9 2>/dev/null || true
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR/work"

# Đồng bộ credentials & browser profiles
cp ~/.amux/accounts.json ~/.amux/identities.json ~/.amux/config.json "$TEST_DIR/" 2>/dev/null || true
ln -s ~/.amux/browser-profiles "$TEST_DIR/browser-profiles" 2>/dev/null || true

# Tạo file mẫu để test subagent
printf 'l1\nl2\nl3\n' > "$TEST_DIR/work/data.txt"

# Hàm dọn dẹp khi script kết thúc hoặc bị ngắt
cleanup() {
    echo ""
    echo "Đang dọn dẹp test gateway trên cổng $TEST_PORT..."
    [ -n "$GATEWAY_PID" ] && kill "$GATEWAY_PID" 2>/dev/null || true
    lsof -ti :"$TEST_PORT" | xargs -r kill -9 2>/dev/null || true
    rm -rf "$TEST_DIR"
    echo "✓ Hoàn tất dọn dẹp. Cổng 8787 vẫn hoạt động bình thường."
}
trap cleanup EXIT INT TERM

# 2. Khởi động Test Gateway trên cổng phụ
echo "Khởi động AMUX Gateway trên cổng $TEST_PORT..."
AMUX_HOME="$TEST_DIR" AM_PROXY_LISTEN="127.0.0.1:$TEST_PORT" amux gateway _daemon > "$TEST_DIR/gateway.log" 2>&1 &
GATEWAY_PID=$!

# Đợi Gateway khởi động
for i in {1..10}; do
    if curl -s "http://127.0.0.1:$TEST_PORT/_am/status" >/dev/null 2>&1; then
        echo "✓ Gateway phụ đã sẵn sàng trên http://127.0.0.1:$TEST_PORT"
        break
    fi
    sleep 0.5
done

# 3. Thực thi kiểm thử
cd "$TEST_DIR/work"

if [ "$MODE" = "-i" ] || [ "$MODE" = "--interactive" ]; then
    echo ""
    echo "=== BẮT ĐẦU PHIÊN TƯƠNG TÁC CLAUDE CODE ==="
    echo "Gõ prompt bạn muốn test. Nhấn Ctrl+D hoặc gõ /exit để thoát."
    echo ""
    AMUX_HOME="$TEST_DIR" AM_PROXY_ADDR="127.0.0.1:$TEST_PORT" AMUX_PORT="$TEST_PORT" \
    amux run claude -p "$TARGET_ID"
else
    echo ""
    echo "=== BẮT ĐẦU CHẠY BENCHMARK 5 BƯỚC ==="
    echo "1) Tạo calc.py có lỗi"
    echo "2) Chạy bash kiểm tra lỗi"
    echo "3) Sửa lỗi bằng Edit tool"
    echo "4) Chạy lại bash xác nhận kết quả"
    echo "5) Gọi subagent đếm số dòng data.txt"
    echo "-------------------------------------------------"

    AMUX_HOME="$TEST_DIR" AM_PROXY_ADDR="127.0.0.1:$TEST_PORT" AMUX_PORT="$TEST_PORT" \
    amux run claude -p "$TARGET_ID" --dangerously-skip-permissions --print \
    "Use your tools for every step, do not guess. 1) Write a file calc.py defining add(a,b) that wrongly returns a-b. 2) Bash: run python3 -c 'import calc;print(calc.add(2,3))' and note the output. 3) Fix the bug in calc.py with the Edit tool. 4) Bash: rerun the same command. 5) Use the Agent tool to launch a subagent that reads data.txt and reports how many lines it has. Final answer: the output before the fix, the output after the fix, and the subagent's line count."
fi
