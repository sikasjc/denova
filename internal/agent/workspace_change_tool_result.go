package agent

import (
	"encoding/json"
	"strings"
)

const workspaceChangeToolResultSchema = "workspace_change.tool_result.v1"

type workspaceChangeToolReceipt struct {
	Schema         string                       `json:"schema"`
	Status         string                       `json:"status"`
	Workspace      string                       `json:"workspace"`
	ChangeGroupID  string                       `json:"change_group_id"`
	ReviewThreadID string                       `json:"review_thread_id"`
	ChangeSetID    string                       `json:"change_set_id"`
	Path           string                       `json:"path"`
	BaseRevision   string                       `json:"base_revision"`
	Revision       string                       `json:"revision"`
	ReviewStatus   string                       `json:"review_status"`
	ApplyState     string                       `json:"apply_state"`
	Edits          []workspaceChangeEditReceipt `json:"edits,omitempty"`
}

type workspaceChangeEditReceipt struct {
	ID           string                       `json:"id,omitempty"`
	Replacements int                          `json:"replacements"`
	Hunks        []workspaceChangeHunkReceipt `json:"hunks,omitempty"`
}

// workspaceChangeHunkReceipt reports one hunk's inclusive 1-based line span in
// the before and after snapshots. Edits applied to files that carry no line
// metadata report zero values, which the JSON encoding omits.
type workspaceChangeHunkReceipt struct {
	BeforeStartLine int `json:"before_start_line,omitempty"`
	BeforeEndLine   int `json:"before_end_line,omitempty"`
	AfterStartLine  int `json:"after_start_line,omitempty"`
	AfterEndLine    int `json:"after_end_line,omitempty"`
}

type workspaceChangeToolModelReceipt struct {
	Path     string                       `json:"path"`
	Revision string                       `json:"revision,omitempty"`
	Edits    []workspaceChangeEditReceipt `json:"edits,omitempty"`
}

// workspaceChangeToolResultForModel 将内部 receipt 转成模型可见版本：
// 隐藏 base_revision（修改前快照），但保留修改后的 revision，
// 让模型能把它作为下一次 edit_file 的 file_revision 链式编辑、免去重复读取。
// 每个 edit 附带修改前后的行号区间，供模型推算后续行号而无需重读文件。
func workspaceChangeToolResultForModel(toolName, content string) string {
	receipt, ok := parseWorkspaceChangeToolReceipt(toolName, content)
	if !ok {
		return content
	}
	public, err := json.Marshal(workspaceChangeToolModelReceipt{
		Path:     receipt.Path,
		Revision: receipt.Revision,
		Edits:    receipt.Edits,
	})
	if err != nil {
		return content
	}
	return string(public)
}

func parseWorkspaceChangeToolReceipt(toolName, content string) (workspaceChangeToolReceipt, bool) {
	if !isWorkspaceChangeReceiptTool(toolName) {
		return workspaceChangeToolReceipt{}, false
	}
	content = strings.TrimSpace(toolResultBody(content))
	if content == "" || !strings.HasPrefix(content, "{") {
		return workspaceChangeToolReceipt{}, false
	}
	var receipt workspaceChangeToolReceipt
	if err := json.Unmarshal([]byte(content), &receipt); err != nil {
		return workspaceChangeToolReceipt{}, false
	}
	if receipt.Schema != workspaceChangeToolResultSchema ||
		strings.TrimSpace(receipt.Workspace) == "" ||
		strings.TrimSpace(receipt.ChangeGroupID) == "" ||
		strings.TrimSpace(receipt.ChangeSetID) == "" ||
		strings.TrimSpace(receipt.Path) == "" {
		return workspaceChangeToolReceipt{}, false
	}
	return receipt, true
}

func toolResultBody(content string) string {
	content = strings.TrimRight(content, "\n")
	for _, separator := range []string{"\n\n" + toolResultMetadataHeader, "\n" + toolResultMetadataHeader} {
		if before, _, ok := strings.Cut(content, separator); ok {
			return strings.TrimRight(before, "\n")
		}
	}
	if strings.HasPrefix(strings.TrimSpace(content), toolResultMetadataHeader) {
		return ""
	}
	return content
}

func isWorkspaceChangeReceiptTool(toolName string) bool {
	switch normalizeToolName(toolName) {
	case "edit_file", "write_file":
		return true
	default:
		return false
	}
}
