package runtime

import (
	"fmt"
	"strings"

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
