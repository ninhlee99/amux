package runtime

import (
	"fmt"
	"strings"
	"time"

	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/types"
)

// LogManifestDiscovery logs discovery of runtime capabilities.
func LogManifestDiscovery(source string, m *RuntimeManifest) {
	if m == nil {
		return
	}
	monitor.AppendEvent("RUNTIME", fmt.Sprintf("%s discovered runtime %s (%d native tools)", source, m.Runtime, len(m.Tools)))
}

// LogToolValidation logs the result of schema validation on tool calls.
func LogToolValidation(source string, calls []types.ToolCall, errs []*ValidationError) {
	if len(calls) == 0 {
		return
	}
	var names []string
	for _, c := range calls {
		names = append(names, c.Name)
	}
	if len(errs) == 0 {
		monitor.AppendEvent("RUNTIME", fmt.Sprintf("%s tools valid: [%s]", source, strings.Join(names, ", ")))
	} else {
		for _, e := range errs {
			monitor.AppendEvent("RUNTIME", fmt.Sprintf("%s tool validation error: %s", source, e.Error()))
		}
	}
}

// LogExecution records a structured execution event for telemetry and debugging.
func LogExecution(reqID, client, runtimeName, toolName, args string, duration time.Duration, exitCode int, err error) {
	status := "OK"
	if err != nil || exitCode != 0 {
		status = fmt.Sprintf("FAIL (exit=%d)", exitCode)
	}
	// Truncate args for compact log
	argsSummary := args
	if len(argsSummary) > 60 {
		argsSummary = argsSummary[:60] + "..."
	}
	monitor.AppendEvent("EXEC", fmt.Sprintf("[%s] %s/%s → %s(%s) %s (%s)", reqID, client, runtimeName, toolName, argsSummary, status, duration.Round(time.Millisecond)))
}
