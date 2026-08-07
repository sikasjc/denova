package agent

import (
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/schema"

	"denova/config"
)

const retainedToolReceiptSchema = "tool_result.retained.v1"
const retainedToolCallSchema = "tool_call.retained.v1"

type retainedToolReceipt struct {
	Schema    string   `json:"schema"`
	ToolName  string   `json:"tool_name"`
	SourceIDs []string `json:"source_ids,omitempty"`
	Names     []string `json:"names,omitempty"`
	StoryID   string   `json:"story_id,omitempty"`
	BranchID  string   `json:"branch_id,omitempty"`
	Path      string   `json:"path,omitempty"`
	Offset    int      `json:"offset,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Note      string   `json:"note"`
}

type retainedToolCall struct {
	Schema         string `json:"schema"`
	ToolName       string `json:"tool_name"`
	Path           string `json:"path,omitempty"`
	OperationCount int    `json:"operation_count,omitempty"`
	ContentOmitted bool   `json:"content_omitted"`
	Note           string `json:"note"`
}

func retainToolContextAcrossTurns(toolName string, policy ToolResultContextPolicy) bool {
	name := normalizeToolName(toolName)
	if strings.TrimSpace(policy.AgentKind) == config.AgentKindInteractiveStory {
		// The next game turn already receives committed TurnResult, StateDelta,
		// RuleResolution and Actor State. Keep only semantic source receipts that
		// tell it what can be re-read; all protocol, filesystem and index tools are
		// transient implementation detail.
		switch name {
		case "read_lore_items":
			return true
		default:
			return false
		}
	}
	switch name {
	case "list_lore_items", "search_story_history", "grep", "glob", "ls", "write_todos":
		return false
	default:
		return true
	}
}

func semanticToolResultContextContent(toolName, content string, _ ToolResultContextPolicy) string {
	if isRetainedToolReceipt(content) {
		return content
	}
	switch normalizeToolName(toolName) {
	case "read_lore_items":
		return retainedLoreReadReceipt(content)
	case "read_file":
		return retainedFileReadReceipt(content)
	case "skill":
		return retainedSkillReceipt(content)
	default:
		return content
	}
}

func isRetainedToolReceipt(content string) bool {
	var envelope struct {
		Schema string `json:"schema"`
	}
	return json.Unmarshal([]byte(content), &envelope) == nil && envelope.Schema == retainedToolReceiptSchema
}

func retainedLoreReadReceipt(content string) string {
	receipt := retainedToolReceipt{
		Schema:   retainedToolReceiptSchema,
		ToolName: "read_lore_items",
		Note:     "Lore bodies were available during the source turn and are omitted from cross-turn context. Re-read an item if exact wording is required.",
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "## "):
			name := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if before, _, ok := strings.Cut(name, "（"); ok {
				name = strings.TrimSpace(before)
			}
			receipt.Names = appendUniqueRetainedValue(receipt.Names, name)
		case strings.HasPrefix(line, "ID："):
			receipt.SourceIDs = appendUniqueRetainedValue(receipt.SourceIDs, strings.TrimSpace(strings.TrimPrefix(line, "ID：")))
		case strings.HasPrefix(line, "ID:"):
			receipt.SourceIDs = appendUniqueRetainedValue(receipt.SourceIDs, strings.TrimSpace(strings.TrimPrefix(line, "ID:")))
		}
	}
	if len(receipt.SourceIDs) == 0 {
		// Do not turn an empty result or tool error into a positive-looking
		// receipt that claims Lore bodies were available.
		return content
	}
	return marshalRetainedToolReceipt(receipt)
}

func retainedFileReadReceipt(content string) string {
	firstLine, _, _ := strings.Cut(content, "\n")
	var metadata workspaceReadFileMetadata
	if json.Unmarshal([]byte(strings.TrimSpace(firstLine)), &metadata) != nil ||
		metadata.Schema != workspaceReadFileResultSchema ||
		strings.TrimSpace(metadata.FilePath) == "" {
		// Tool errors and results from an unknown read_file implementation must
		// remain visible instead of being misrepresented as a successful read.
		return content
	}
	receipt := retainedToolReceipt{
		Schema:   retainedToolReceiptSchema,
		ToolName: "read_file",
		Path:     strings.TrimSpace(metadata.FilePath),
		Offset:   metadata.Offset,
		Limit:    metadata.Limit,
		Note:     "The selected file body was available during the source turn and is omitted from cross-turn context. Re-read this path and window for current exact wording.",
	}
	return marshalRetainedToolReceipt(receipt)
}

func retainedSkillReceipt(content string) string {
	const (
		namePrefix = "Launching skill:"
		pathPrefix = "Base directory for this skill:"
	)
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return content
	}
	name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), namePrefix))
	path := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[1]), pathPrefix))
	if name == "" || path == "" ||
		!strings.HasPrefix(strings.TrimSpace(lines[0]), namePrefix) ||
		!strings.HasPrefix(strings.TrimSpace(lines[1]), pathPrefix) {
		// Unknown or failed skill results remain intact so the next turn does
		// not mistake an error for a successfully loaded methodology.
		return content
	}
	return marshalRetainedToolReceipt(retainedToolReceipt{
		Schema:   retainedToolReceiptSchema,
		ToolName: "skill",
		Names:    []string{name},
		Path:     path,
		Note:     "The Skill body was available during the source turn and is omitted from cross-turn context. Load the Skill again if its exact workflow is required.",
	})
}

func semanticToolCallContextArguments(toolName, arguments string) string {
	name := normalizeToolName(toolName)
	switch name {
	case "write_file", "edit_file", "write_lore_items":
	default:
		return arguments
	}
	var payload map[string]any
	if json.Unmarshal([]byte(arguments), &payload) != nil {
		return arguments
	}
	path := toolPathFromArgs(arguments)
	if name != "write_lore_items" && strings.TrimSpace(path) == "" {
		return arguments
	}
	operationCount := 1
	if name == "write_file" {
		if _, ok := payload["content"].(string); !ok {
			return arguments
		}
	} else if name == "edit_file" {
		edits, ok := payload["edits"].([]any)
		if !ok || len(edits) == 0 {
			return arguments
		}
		operationCount = len(edits)
	} else if name == "write_lore_items" {
		items, _ := payload["items"].([]any)
		deleted, _ := payload["delete_ids"].([]any)
		operationCount = len(items) + len(deleted)
		if operationCount == 0 {
			return arguments
		}
	}
	projected, err := json.Marshal(retainedToolCall{
		Schema:         retainedToolCallSchema,
		ToolName:       name,
		Path:           path,
		OperationCount: operationCount,
		ContentOmitted: true,
		Note:           "The write body, patch, or Lore payload was available during the source turn and is omitted from cross-turn context. Re-read the current source of truth for authoritative content.",
	})
	if err != nil {
		return arguments
	}
	return string(projected)
}

func shouldProjectToolCallContextArguments(toolName, resultContent string) bool {
	switch normalizeToolName(toolName) {
	case "write_file", "edit_file":
		receipt, ok := parseWorkspaceChangeToolReceipt(toolName, resultContent)
		return ok &&
			strings.EqualFold(strings.TrimSpace(receipt.Status), "applied") &&
			strings.EqualFold(strings.TrimSpace(receipt.ApplyState), "applied")
	case "write_lore_items":
		itemIDs, deletedIDs := parseWriteLoreItemsToolResult(toolName, resultContent)
		return len(itemIDs) > 0 || len(deletedIDs) > 0
	default:
		return false
	}
}

func marshalRetainedToolReceipt(receipt retainedToolReceipt) string {
	data, err := json.Marshal(receipt)
	if err != nil {
		return ""
	}
	return string(data)
}

func appendUniqueRetainedValue(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func filterSemanticToolContextMessages(messages []*schema.Message, policy ToolResultContextPolicy) []*schema.Message {
	type retainedCall struct {
		toolName  string
		arguments string
		retain    bool
		valid     bool
	}
	callsByID := make(map[string]retainedCall)
	resultCountsByID := make(map[string]int)
	resultContentByID := make(map[string]string)
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		if msg.Role == schema.Assistant {
			for _, call := range msg.ToolCalls {
				callID := strings.TrimSpace(call.ID)
				toolName := normalizeToolName(call.Function.Name)
				if callID == "" || toolName == "" {
					continue
				}
				if existing, exists := callsByID[callID]; exists {
					existing.valid = false
					callsByID[callID] = existing
					continue
				}
				arguments, valid := retainedToolCallArguments(call.Function.Arguments)
				callsByID[callID] = retainedCall{
					toolName:  toolName,
					arguments: arguments,
					retain:    retainToolContextAcrossTurns(toolName, policy),
					valid:     valid,
				}
			}
			continue
		}
		if msg.Role == schema.Tool {
			callID := strings.TrimSpace(msg.ToolCallID)
			if callID != "" {
				resultCountsByID[callID]++
				if resultCountsByID[callID] == 1 {
					resultContentByID[callID] = msg.Content
				}
			}
		}
	}

	filtered := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		switch msg.Role {
		case schema.Assistant:
			if len(msg.ToolCalls) == 0 {
				filtered = append(filtered, msg)
				continue
			}
			next := *msg
			next.ToolCalls = nil
			for _, call := range msg.ToolCalls {
				callID := strings.TrimSpace(call.ID)
				callPolicy, knownCall := callsByID[callID]
				if callID == "" || !knownCall || !callPolicy.valid || resultCountsByID[callID] != 1 || !callPolicy.retain {
					continue
				}
				arguments := callPolicy.arguments
				if shouldProjectToolCallContextArguments(callPolicy.toolName, resultContentByID[callID]) {
					arguments = semanticToolCallContextArguments(callPolicy.toolName, arguments)
				}
				call.Function.Arguments = arguments
				next.ToolCalls = append(next.ToolCalls, call)
			}
			if len(next.ToolCalls) > 0 || strings.TrimSpace(next.Content) != "" {
				filtered = append(filtered, &next)
			}
		case schema.Tool:
			callID := strings.TrimSpace(msg.ToolCallID)
			callPolicy, ok := callsByID[callID]
			if callID == "" || !ok || !callPolicy.valid || resultCountsByID[callID] != 1 || !callPolicy.retain {
				continue
			}
			next := *msg
			// Provider-restored histories may omit ToolName on result messages.
			// Resolve it from the paired assistant call so filtering and semantic
			// compaction always make the same decision for both halves.
			next.ToolName = callPolicy.toolName
			filtered = append(filtered, sanitizedToolContextMessage(&next, policy))
		default:
			filtered = append(filtered, msg)
		}
	}
	return filtered
}
