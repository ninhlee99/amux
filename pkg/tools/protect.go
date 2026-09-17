package tools

import (
	"regexp"
	"strings"
)

// ProtectedPattern defines syntax markers that must NEVER be stripped, sanitized, or truncated.
var ProtectedXMLTags = []string{
	"tools", "tool_description", "tool_name", "parameters",
	"function_calls", "invoke", "parameter",
	"function_results", "result", "error",
	"mcp_tools", "mcp_resource", "use_mcp_tool",
	"skills", "available_skills", "skill_definition",
	"merchant_data", "context", "environment_context", "system_information",
	"git_status", "directory_structure",
	"thinking", "thought", "antThinking",
}

var (
	reShopifyGID   = regexp.MustCompile(`gid://shopify/[A-Za-z0-9_]+/\d+`)
	reGitCommitSHA = regexp.MustCompile(`\b[0-9a-f]{40}\b|\b[0-9a-f]{7}\b`)
	reFileHash     = regexp.MustCompile(`\b(?:sha256|sha1|md5):[0-9a-fA-F]+\b`)
	reDiffHeader   = regexp.MustCompile(`(?m)^--- a/.*$|^\+\+\+ b/.*$|^@@ -\d+,\d+ \+\d+,\d+ @@$`)
)

// ContainsProtectedPattern checks if the payload contains any syntax pattern that requires verbatim preservation.
func ContainsProtectedPattern(content string) bool {
	for _, tag := range ProtectedXMLTags {
		if strings.Contains(content, "<"+tag+">") || strings.Contains(content, "</"+tag+">") {
			return true
		}
	}
	if reShopifyGID.MatchString(content) {
		return true
	}
	if reDiffHeader.MatchString(content) {
		return true
	}
	return false
}

// AssertProtectedSyntaxIntegrity verifies that expected protected markers are present in candidate.
func AssertProtectedSyntaxIntegrity(original, processed string) []string {
	var violations []string

	// Check XML tags
	for _, tag := range ProtectedXMLTags {
		openTag := "<" + tag + ">"
		closeTag := "</" + tag + ">"
		if strings.Contains(original, openTag) && !strings.Contains(processed, openTag) {
			violations = append(violations, "missing open tag: "+openTag)
		}
		if strings.Contains(original, closeTag) && !strings.Contains(processed, closeTag) {
			violations = append(violations, "missing close tag: "+closeTag)
		}
	}

	// Check Shopify GIDs
	origGIDs := reShopifyGID.FindAllString(original, -1)
	for _, gid := range origGIDs {
		if !strings.Contains(processed, gid) {
			violations = append(violations, "missing shopify GID: "+gid)
		}
	}

	return violations
}
