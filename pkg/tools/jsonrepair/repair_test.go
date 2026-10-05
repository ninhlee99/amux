package jsonrepair

import (
	"encoding/json"
	"testing"
)

func TestRepair(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validOut bool
	}{
		{
			name:     "Valid JSON untouched",
			input:    `{"name":"Bash","arguments":{"command":"ls -la"}}`,
			validOut: true,
		},
		{
			name:     "Unclosed object braces",
			input:    `{"name":"Bash","arguments":{"command":"ls -la"`,
			validOut: true,
		},
		{
			name:     "Unclosed string and braces",
			input:    `{"name":"Bash","arguments":{"command":"git status`,
			validOut: true,
		},
		{
			name:     "Trailing commas",
			input:    `{"name":"Bash","arguments":{"command":"ls -la",},}`,
			validOut: true,
		},
		{
			name:     "Single quotes",
			input:    `{'name':'Bash','arguments':{'command':'echo "hello"'}}`,
			validOut: true,
		},
		{
			name:     "Python constants",
			input:    `{"enabled": True, "debug": False, "data": None}`,
			validOut: true,
		},
		{
			name:     "Comments inside JSON",
			input:    "{\n// this is a tool\n\"name\": \"Bash\", /* inline */ \"arguments\": {\"command\": \"pwd\"}\n}",
			validOut: true,
		},
		{
			name:     "Literal newlines inside string",
			input:    "{\"command\": \"line 1\nline 2\"}",
			validOut: true,
		},
		{
			name:     "Markdown code fence ```json",
			input:    "```json\n{\"name\":\"Bash\",\"arguments\":{\"command\":\"ls -la\"}}\n```",
			validOut: true,
		},
		{
			name:     "Markdown code fence ``` with trailing comma",
			input:    "```\n{\"name\":\"Bash\",\"arguments\":{\"command\":\"ls -la\",},}\n```",
			validOut: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repaired := Repair(tc.input)
			if !json.Valid([]byte(repaired)) {
				t.Fatalf("Repair(%q) produced invalid JSON: %s", tc.input, repaired)
			}
		})
	}
}
