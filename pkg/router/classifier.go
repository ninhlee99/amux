package router

import (
	"regexp"
	"strings"

	"amux-accounts/pkg/types"
)

// Hidden task intents — soft routing hints only. Any account may still handle
// any task; preferred groups are tried first among eligible adapters.
const (
	TaskGeneral  = "general"
	TaskCoding   = "coding"
	TaskAnalysis = "analysis"
	TaskPlan     = "plan"
	TaskClarify  = "clarify"
	TaskReview   = "review"
	TaskCompact  = "compact"
	TaskQuality  = "quality"
	TaskFix      = "fix"
)

// TaskClassification holds the analysis result of a ChatRequest.
type TaskClassification struct {
	// Kind is the soft task intent (coding/analysis/…). Empty means general.
	Kind string
	// IsHeavy indicates the task requires deep reasoning or complex analysis,
	// warranting escalation to a Pro tier model (e.g. gemini-3.1-pro / gemini-2.5-pro / o3-mini).
	IsHeavy bool
	// NeedsThinking indicates whether extended thinking / reasoning tokens should be activated.
	NeedsThinking bool
	// ReasoningEffort is "low", "medium", or "high".
	ReasoningEffort string
	// Reasons lists the signals triggering this classification (for logs/monitoring).
	Reasons []string
}

var (
	// Fast/trivial prompts that should not trigger heavy thinking.
	reTrivialPrompt = regexp.MustCompile(`(?i)^(?:hi|hello|hey|chào|ok|okay|yes|no|được|tiếp tục|continue|git status|pwd|ls|dir|help)\b`)

	// Keywords indicating deep analysis, architecture, or security audit.
	reHeavyKeywords = regexp.MustCompile(`(?i)\b(?:` +
		`architecture|kiến\s*trúc|system\s*design|thiết\s*kế\s*hệ\s*thống|` +
		`audit|security|bảo\s*mật|vulnerability|lỗ\s*hổng|cve|penetration|` +
		`code\s*review|đánh\s*giá\s*code|phân\s*tích\s*chuyên\s*sâu|deep\s*analysis|` +
		`root\s*cause|nguyên\s*nhân\s*gốc|race\s*condition|deadlock|memory\s*leak|` +
		`refactor|tái\s*cấu\s*trúc|tối\s*ưu\s*hiệu\s*năng|performance\s*optimization|` +
		`algorithm|thuật\s*toán|complexity|formal\s*proof|chứng\s*minh|` +
		`multi-agent|suy\s*luận\s*từng\s*bước|step\s*by\s*step\s*reasoning|` +
		`investigate\s*why|tại\s*sao\s*bị\s*lỗi` +
		`)\b`)

	rePlanIntent = regexp.MustCompile(`(?i)\b(?:` +
		`lên\s*kế\s*hoạch|kế\s*hoạch|lập\s*kế\s*hoạch|plan|planning|/plan|roadmap|` +
		`chiến\s*lược|strategy|proposal|đề\s*xuất|thiết\s*kế\s*giải\s*pháp|solution\s*design|` +
		`architecture\s*plan|phác\s*thảo|outline|workflow|quy\s*trình|bước\s*triển\s*khai|implementation\s*plan|migration\s*plan` +
		`)\b`)

	reClarifyIntent = regexp.MustCompile(`(?i)\b(?:` +
		`làm\s*rõ\s*yêu\s*cầu|làm\s*rõ|clarify|clarification|/grill-me|interview|phỏng\s*vấn|` +
		`hỏi\s*thêm|câu\s*hỏi|question|yêu\s*cầu\s*chi\s*tiết|requirement\s*clarification|` +
		`xác\s*nhận\s*lại|confirm\s*requirement|thắc\s*mắc|trao\s*đổi\s*yêu\s*cầu` +
		`)\b`)

	reAnalysisIntent = regexp.MustCompile(`(?i)\b(?:` +
		`phân\s*tích|analyze|analysis|explain|giải\s*thích|hiểu\s*code|understand|` +
		`investigate|điều\s*tra|explore|tìm\s*hiểu|overview|tổng\s*quan` +
		`)\b`)

	reReviewIntent = regexp.MustCompile(`(?i)\b(?:` +
		`review|code\s*review|kiểm\s*tra\s*(?:pr|code|diff)|pull\s*request|\bpr\b|` +
		`nhận\s*xét|đánh\s*giá\s*(?:code|pr|diff)` +
		`)\b`)

	reCompactIntent = regexp.MustCompile(`(?i)\b(?:` +
		`tóm\s*tắt|summarize|summary|compact|rút\s*gọn|condense|tl;?dr` +
		`)\b`)

	reQualityIntent = regexp.MustCompile(`(?i)\b(?:` +
		`quality|chất\s*lượng|evaluate\s*ux|đánh\s*giá\s*(?:sản\s*phẩm|ux|ui)|` +
		`product\s*review|usability` +
		`)\b`)

	reFixIntent = regexp.MustCompile(`(?i)\b(?:` +
		`fix\s*bug|sửa\s*lỗi|bugfix|hotfix|patch|resolve\s*error|` +
		`broken|không\s*chạy|failing\s*test|flaky` +
		`)\b`)

	reCodingIntent = regexp.MustCompile(`(?i)\b(?:` +
		`implement|viết\s*code|write\s*(?:code|a\s*function|tests?)|` +
		`add\s*(?:feature|endpoint|function)|create\s*(?:file|component)|` +
		`refactor|generate\s*code|coding` +
		`)\b`)

	// Stack trace or crash dump signatures in tool results.
	reCrashOrStackTrace = regexp.MustCompile(`(?i)(?:panic:|Traceback \(most recent call last\):|fatal error:|NullPointerException|SIGSEGV|segmentation fault)`)

	// Explicit pro / reasoning model names requested by client.
	reProModelName = regexp.MustCompile(`(?i)(?:o1|o3|gemini-(?:3\.1|2\.5|1\.5)-pro|claude-3-7-sonnet|deepseek-r1|\br1\b|kimi-(?:k\d+|latest|thinking|research)[\w.-]*|moonshot-v1(?:-\w+)?|grok-(?:2|3|beta)[\w.-]*)`)

	mutatingToolNames = map[string]bool{
		"bash": true, "edit": true, "write": true, "notebookedit": true,
		"exec_command": true, "apply_diff": true, "write_to_file": true,
		"run_command": true, "delete": true, "movefile": true,
	}
)

// ClassifyTask evaluates an incoming ChatRequest for pro/thinking escalation
// and a soft task Kind used only for account-group preference.
func ClassifyTask(req *types.ChatRequest) TaskClassification {
	if req == nil {
		return TaskClassification{Kind: TaskGeneral, ReasoningEffort: "medium"}
	}

	var res TaskClassification
	res.Kind = TaskGeneral
	res.ReasoningEffort = "medium"

	// 1. Explicit client signals.
	if req.Thinking {
		res.NeedsThinking = true
		res.Reasons = append(res.Reasons, "client requested thinking mode")
	}
	if strings.EqualFold(req.TargetTier, "pro") {
		res.IsHeavy = true
		res.Reasons = append(res.Reasons, "client requested pro tier")
	}
	if req.ReasoningEffort != "" {
		res.ReasoningEffort = req.ReasoningEffort
		res.NeedsThinking = true
	}
	if req.Model != "" && reProModelName.MatchString(req.Model) {
		res.IsHeavy = true
		res.NeedsThinking = true
		res.Reasons = append(res.Reasons, "model name specifies pro/reasoning tier: "+req.Model)
	}

	// Extract prompt texts from messages.
	var lastUserText string
	var totalLen int
	var codeBlockCount int
	var hasStackTrace bool
	var hasDiff bool

	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		totalLen += len(m.Content)
		if lastUserText == "" && strings.EqualFold(m.Role, "user") && m.Content != "" {
			lastUserText = strings.TrimSpace(m.Content)
		}
		if !hasStackTrace && reCrashOrStackTrace.MatchString(m.Content) {
			hasStackTrace = true
		}
		if !hasDiff && (strings.Contains(m.Content, "diff --git") || strings.Contains(m.Content, "@@ -")) {
			hasDiff = true
		}
		codeBlockCount += strings.Count(m.Content, "```")
	}

	hasMutating := requestHasMutatingTools(req)

	// Soft task intent (keywords win over generic tool catalogs).
	res.Kind = detectTaskKind(lastUserText, hasMutating, hasDiff, hasStackTrace)
	if res.Kind != TaskGeneral {
		res.Reasons = append(res.Reasons, "task kind: "+res.Kind)
	}

	// 2. Trivial prompt check — do not auto-escalate trivial greetings or quick commands
	// unless explicitly requested by the client.
	if len(lastUserText) < 50 && reTrivialPrompt.MatchString(lastUserText) && !req.Thinking && !strings.EqualFold(req.TargetTier, "pro") {
		return res
	}

	// 3. Keyword-based heavy task detection.
	keywordCount := 0
	if lastUserText != "" && reHeavyKeywords.MatchString(lastUserText) {
		res.IsHeavy = true
		res.NeedsThinking = true
		matches := reHeavyKeywords.FindAllString(lastUserText, 5)
		keywordCount = len(matches)
		res.Reasons = append(res.Reasons, "heavy task keywords: "+strings.Join(matches, ", "))
		if res.Kind == TaskGeneral {
			res.Kind = TaskAnalysis
		}
	}

	// 4. Code & context volume heuristic.
	if hasDiff && (strings.Contains(strings.ToLower(lastUserText), "review") || strings.Contains(strings.ToLower(lastUserText), "kiểm tra")) {
		res.IsHeavy = true
		res.NeedsThinking = true
		res.Reasons = append(res.Reasons, "code diff review task")
		if res.Kind == TaskGeneral {
			res.Kind = TaskReview
		}
	}

	if hasStackTrace {
		res.IsHeavy = true
		res.NeedsThinking = true
		res.Reasons = append(res.Reasons, "stack trace / crash investigation")
		if res.Kind == TaskGeneral || res.Kind == TaskCoding {
			res.Kind = TaskFix
		}
	}

	// Long conversation context or multiple substantial code blocks (> 3000 chars of code)
	if codeBlockCount >= 4 && totalLen > 8000 {
		res.IsHeavy = true
		res.Reasons = append(res.Reasons, "large multi-turn code context")
	}

	// Calibrate reasoning effort
	if keywordCount >= 2 || len(res.Reasons) >= 2 || (res.IsHeavy && (hasStackTrace || hasDiff)) {
		res.ReasoningEffort = "high"
	}

	return res
}

func detectTaskKind(text string, hasMutating, hasDiff, hasStackTrace bool) string {
	if text != "" {
		switch {
		case reFixIntent.MatchString(text) || hasStackTrace:
			return TaskFix
		case rePlanIntent.MatchString(text) && !reCodingIntent.MatchString(text):
			return TaskPlan
		case reClarifyIntent.MatchString(text) && !reCodingIntent.MatchString(text):
			return TaskClarify
		case reReviewIntent.MatchString(text) && (hasDiff || !hasMutating):
			return TaskReview
		case reCompactIntent.MatchString(text):
			return TaskCompact
		case reQualityIntent.MatchString(text):
			return TaskQuality
		case reAnalysisIntent.MatchString(text) && !hasMutating:
			return TaskAnalysis
		case reAnalysisIntent.MatchString(text) && !reCodingIntent.MatchString(text):
			// "phân tích" with a full agent tool catalog still leans analysis.
			return TaskAnalysis
		case rePlanIntent.MatchString(text):
			return TaskPlan
		case reClarifyIntent.MatchString(text):
			return TaskClarify
		case reCodingIntent.MatchString(text):
			return TaskCoding
		case reReviewIntent.MatchString(text):
			return TaskReview
		}
	}
	if hasMutating {
		return TaskCoding
	}
	if hasDiff {
		return TaskReview
	}
	return TaskGeneral
}

// IsWebTask reports whether the task should be routed preferentially to web proxies
// (planning, clarification, analysis, review, compaction, quality evaluation) to conserve
// subscription quotas and API costs.
func IsWebTask(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case TaskAnalysis, TaskPlan, TaskClarify, TaskReview, TaskCompact, TaskQuality:
		return true
	default:
		return false
	}
}

func requestHasMutatingTools(req *types.ChatRequest) bool {
	if req == nil {
		return false
	}
	for _, t := range req.Tools {
		name := strings.ToLower(strings.TrimSpace(t.Name))
		if mutatingToolNames[name] {
			return true
		}
		// MCP / namespaced tools: mcp__x__write_file
		base := name
		if i := strings.LastIndex(name, "__"); i >= 0 && i+2 < len(name) {
			base = name[i+2:]
		}
		if mutatingToolNames[base] {
			return true
		}
	}
	return false
}
