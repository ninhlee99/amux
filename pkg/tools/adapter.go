package tools

// ToolAdapter defines bidirectional conversion between provider-specific wire schemas
// and the canonical Universal Tool Model.
type ToolAdapter interface {
	Name() string
	ToUniversalTools(raw []byte) ([]UniversalTool, error)
	FromUniversalTools(tools []UniversalTool) (any, error)
	ToUniversalToolCalls(raw any) ([]UniversalToolCall, error)
	FromUniversalToolCalls(calls []UniversalToolCall) (any, error)
	ToUniversalToolResults(raw any) ([]UniversalToolResult, error)
	FromUniversalToolResults(results []UniversalToolResult) (any, error)
}
