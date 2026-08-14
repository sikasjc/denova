package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"

	"denova/internal/workspacechange"
)

var workspaceEditFileToolDescription = strings.TrimSpace(`Apply one or more line-based or exact text edits to a single workspace file as one reviewed change.
- file_path must identify one file inside the current workspace.
- Prefer start_line/end_line with new_string. read_file returns 1-based line numbers and a revision for this purpose; copy that revision to file_revision. end_line is inclusive and defaults to start_line.
- A line edit replaces complete source lines. The tool preserves the file's line-ending style and keeps the following line separate when replacing a newline-terminated range. An empty new_string deletes the selected lines.
- Use old_string/new_string only when line targeting is unsuitable. Copy old_string verbatim from the latest read, including punctuation, quotes, spaces and line breaks.
- Every item in edits is resolved against the same original file snapshot, not against the result of an earlier item.
- Keep edits non-overlapping. Use replace_all only when every exact occurrence should change.
- Put dependent changes to the same file in one call. Independent files may use separate edit_file calls in the same assistant response.
- The tool captures and protects the current file snapshot internally when the call starts.
- If a line range is invalid, exact matching fails, or the revision changed, read the file again and rebuild the edit from current numbered output. Do not fall back to a full-file replacement.

将一个或多个按行或精确文本修改作为一次可审阅变更应用到同一个 workspace 文件。
- file_path 必须指向当前 workspace 内的单个文件。
- 优先使用 start_line/end_line 和 new_string；read_file 会为此返回从 1 开始的行号与 revision，请把该 revision 传入 file_revision。end_line 包含在替换范围内，省略时等于 start_line。
- 按行修改会替换完整源文件行，工具会保持文件换行符风格；被替换范围原本带换行符时会保持下一行独立。new_string 为空表示删除所选行。
- 仅在行号不适合定位时使用 old_string/new_string；old_string 必须从最新读取结果逐字复制并保留标点、引号、空格和换行。
- edits 中的每一项都基于同一份原始文件快照解析，不基于前一项修改后的结果。
- 各修改区间不得重叠；只有确实需要替换全部精确匹配时才使用 replace_all。
- 同一文件内相互依赖的修改必须放在一次调用中；不同文件的独立修改可以在同一轮分别调用 edit_file。
- 工具会在调用开始时自行获取并保护当前文件快照。
- 如果行号范围无效、精确匹配失败或 revision 已变化，重新读取文件并根据最新带行号结果重建 edit；不要降级为整文件覆盖。`)

var workspaceWriteFileToolDescription = strings.TrimSpace(`Replace the complete content of one workspace file as a reviewed change.
- Use edit_file for localized changes; use write_file only for a new file or an intentional full rewrite.
- Multiple review comments or a failed exact edit do not by themselves authorize a full rewrite of an existing chapter.
- file_path must identify one file inside the current workspace.
- The tool detects whether the file exists and protects its current snapshot internally.

将一个 workspace 文件的完整内容替换为新内容，并记录为可审阅变更。
- 局部修改使用 edit_file；只有新建文件或明确需要整体重写时才使用 write_file。
- 审阅意见很多或 edit_file 精确匹配失败，都不等于获得覆盖已有章节的授权。
- file_path 必须指向当前 workspace 内的单个文件。
- 工具会自行判断文件是否存在并保护当前快照。`)

type workspaceChangeService interface {
	Workspace() string
	ReadFile(string) (content string, revision string, err error)
	ApplyEdits(context.Context, workspacechange.ApplyEditsRequest) (workspacechange.ChangeSet, error)
	ReplaceFile(context.Context, workspacechange.ReplaceFileRequest) (workspacechange.ChangeSet, error)
}

type workspaceEditFileInput struct {
	FilePath     string                      `json:"file_path" jsonschema:"required,description=Absolute or workspace-relative path of the single file to edit"`
	FileRevision string                      `json:"file_revision,omitempty" jsonschema:"description=Full-file revision returned by read_file; required for line-based edits so stale line numbers are rejected"`
	Edits        []workspaceEditFileTextEdit `json:"edits" jsonschema:"required,description=One or more non-overlapping line-based or exact replacements evaluated against the same original file snapshot"`
}

type workspaceEditFileTextEdit struct {
	ID         string `json:"id,omitempty" jsonschema:"description=Optional stable identifier used to associate review comments with this edit"`
	StartLine  int    `json:"start_line,omitempty" jsonschema:"description=Preferred 1-based first complete source line to replace"`
	EndLine    int    `json:"end_line,omitempty" jsonschema:"description=Inclusive 1-based last source line; defaults to start_line"`
	OldString  string `json:"old_string,omitempty" jsonschema:"description=Fallback exact non-empty text selector; mutually exclusive with start_line/end_line"`
	NewString  string `json:"new_string" jsonschema:"description=Replacement text; an empty string deletes the matched text"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"description=With old_string only, replace every exact occurrence; defaults to false"`
}

type workspaceWriteFileInput struct {
	FilePath string `json:"file_path" jsonschema:"required,description=Absolute or workspace-relative path of the file to replace"`
	Content  string `json:"content" jsonschema:"description=Complete new file content"`
}

func newWorkspaceEditFileTool(changes workspaceChangeService) (tool.BaseTool, error) {
	if changes == nil {
		return nil, fmt.Errorf("workspace change service is nil")
	}
	workspace, err := canonicalChangeWorkspace(changes)
	if err != nil {
		return nil, err
	}
	return utils.InferTool("edit_file", workspaceEditFileToolDescription, func(ctx context.Context, input workspaceEditFileInput) (string, error) {
		baseRevision := strings.TrimSpace(input.FileRevision)
		if containsLineBasedEdit(input.Edits) && baseRevision == "" {
			return "", &workspacechange.Error{
				Code:    workspacechange.ErrorCodeInvalidEdit,
				Message: "file_revision from read_file is required for line-based edits",
				Details: map[string]any{
					"path":              input.FilePath,
					"field":             "file_revision",
					"workspace_mutated": false,
				},
			}
		}
		if baseRevision == "" {
			var err error
			baseRevision, err = currentWorkspaceBaseRevision(changes, input.FilePath)
			if err != nil {
				return "", err
			}
		}
		edits := make([]workspacechange.TextEdit, 0, len(input.Edits))
		for _, edit := range input.Edits {
			edits = append(edits, workspacechange.TextEdit{
				ID:         edit.ID,
				StartLine:  edit.StartLine,
				EndLine:    edit.EndLine,
				OldString:  edit.OldString,
				NewString:  edit.NewString,
				ReplaceAll: edit.ReplaceAll,
			})
		}
		changeSet, err := changes.ApplyEdits(ctx, workspacechange.ApplyEditsRequest{
			Path:         input.FilePath,
			BaseRevision: baseRevision,
			Edits:        edits,
			Metadata:     workspaceChangeMetadata(ctx),
		})
		if err != nil {
			return "", err
		}
		return marshalWorkspaceChangeToolReceipt(workspace, changeSet)
	})
}

func containsLineBasedEdit(edits []workspaceEditFileTextEdit) bool {
	for _, edit := range edits {
		if edit.StartLine != 0 || edit.EndLine != 0 {
			return true
		}
	}
	return false
}

func newWorkspaceWriteFileTool(changes workspaceChangeService) (tool.BaseTool, error) {
	if changes == nil {
		return nil, fmt.Errorf("workspace change service is nil")
	}
	workspace, err := canonicalChangeWorkspace(changes)
	if err != nil {
		return nil, err
	}
	return utils.InferTool("write_file", workspaceWriteFileToolDescription, func(ctx context.Context, input workspaceWriteFileInput) (string, error) {
		baseRevision, err := currentWorkspaceBaseRevisionOrMissing(changes, input.FilePath)
		if err != nil {
			return "", err
		}
		changeSet, err := changes.ReplaceFile(ctx, workspacechange.ReplaceFileRequest{
			Path:         input.FilePath,
			Content:      input.Content,
			BaseRevision: baseRevision,
			Metadata:     workspaceChangeMetadata(ctx),
		})
		if err != nil {
			return "", err
		}
		return marshalWorkspaceChangeToolReceipt(workspace, changeSet)
	})
}

func canonicalChangeWorkspace(changes workspaceChangeService) (string, error) {
	workspace := strings.TrimSpace(changes.Workspace())
	if workspace == "" {
		return "", fmt.Errorf("workspace change service has no workspace identity")
	}
	if !filepath.IsAbs(workspace) {
		return "", fmt.Errorf("workspace change service path is not absolute: %s", workspace)
	}
	return filepath.Clean(workspace), nil
}

func currentWorkspaceBaseRevision(changes workspaceChangeService, path string) (string, error) {
	_, revision, err := changes.ReadFile(path)
	if err != nil {
		return "", err
	}
	revision = strings.TrimSpace(revision)
	if revision != "" {
		return revision, nil
	}
	return "", &workspacechange.Error{
		Code:    workspacechange.ErrorCodeConflict,
		Message: "workspace change service returned an empty current revision",
		Details: map[string]any{"path": path, "workspace_mutated": false},
	}
}

func currentWorkspaceBaseRevisionOrMissing(changes workspaceChangeService, path string) (string, error) {
	revision, err := currentWorkspaceBaseRevision(changes, path)
	if err == nil {
		return revision, nil
	}
	var changeErr *workspacechange.Error
	if errors.As(err, &changeErr) && changeErr.Code == workspacechange.ErrorCodeNotFound {
		return "missing", nil
	}
	return "", err
}

func workspaceChangeMetadata(ctx context.Context) workspacechange.ChangeMetadata {
	callID := strings.TrimSpace(compose.GetToolCallID(ctx))
	runID := ""
	sessionID := ""
	reviewThreadID := ""
	if observer := RunObserverFromContext(ctx); observer != nil {
		runID = strings.TrimSpace(observer.RunID())
		sessionID = strings.TrimSpace(observer.SessionID())
		reviewThreadID = strings.TrimSpace(observer.ReviewThreadID())
	}
	groupID := runID
	if groupID == "" {
		groupID = callID
	}
	return workspacechange.ChangeMetadata{
		Origin:         workspacechange.OriginAgent,
		ChangeGroupID:  groupID,
		RunID:          runID,
		SessionID:      sessionID,
		ReviewThreadID: reviewThreadID,
		ToolCallID:     callID,
	}
}

func marshalWorkspaceChangeToolReceipt(workspace string, changeSet workspacechange.ChangeSet) (string, error) {
	receipt := workspaceChangeToolReceipt{
		Schema:         workspaceChangeToolResultSchema,
		Status:         workspaceChangeReceiptStatus(changeSet),
		Workspace:      workspace,
		ChangeGroupID:  changeSet.GroupID,
		ReviewThreadID: changeSet.ReviewThreadID,
		ChangeSetID:    changeSet.ID,
		Path:           changeSet.Path,
		BaseRevision:   changeSet.BaseRevision,
		Revision:       changeSet.Revision,
		ReviewStatus:   changeSet.ReviewStatus,
		ApplyState:     changeSet.ApplyState,
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return "", fmt.Errorf("serialize workspace change receipt: %w", err)
	}
	return string(data), nil
}

func workspaceChangeReceiptStatus(changeSet workspacechange.ChangeSet) string {
	if strings.TrimSpace(changeSet.ApplyState) == "" || changeSet.ApplyState == workspacechange.ApplyStateApplied {
		return "applied"
	}
	return changeSet.ApplyState
}

type workspaceChangeToolErrorReceipt struct {
	Schema           string         `json:"schema"`
	Status           string         `json:"status"`
	Tool             string         `json:"tool"`
	Code             string         `json:"code"`
	Message          string         `json:"message"`
	Details          map[string]any `json:"details,omitempty"`
	Retryable        bool           `json:"retryable"`
	WorkspaceMutated bool           `json:"workspace_mutated"`
}

func formatWorkspaceChangeToolError(toolName string, err error) (string, bool) {
	var changeErr *workspacechange.Error
	if !errors.As(err, &changeErr) || changeErr == nil {
		return "", false
	}
	receipt := workspaceChangeToolErrorReceipt{
		Schema:           "workspace_change.tool_error.v1",
		Status:           "rejected",
		Tool:             normalizeToolName(toolName),
		Code:             changeErr.Code,
		Message:          workspaceChangeToolPublicErrorMessage(changeErr),
		Details:          workspaceChangeToolPublicErrorDetails(changeErr.Details),
		Retryable:        workspaceChangeErrorRetryable(changeErr.Code),
		WorkspaceMutated: workspaceChangeErrorMutated(changeErr),
	}
	data, marshalErr := json.Marshal(receipt)
	if marshalErr != nil {
		return "", false
	}
	return "[tool error]\n" + string(data), true
}

func workspaceChangeToolPublicErrorMessage(changeErr *workspacechange.Error) string {
	if changeErr != nil && changeErr.Code == workspacechange.ErrorCodeRevisionConflict {
		return "Workspace file changed during the tool call; retry the operation. / 工具调用期间文件发生变化，请重试。"
	}
	if changeErr == nil {
		return ""
	}
	return changeErr.Message
}

func workspaceChangeToolPublicErrorDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	public := make(map[string]any, len(details))
	for key, value := range details {
		if strings.Contains(strings.ToLower(key), "revision") {
			continue
		}
		public[key] = value
	}
	if len(public) == 0 {
		return nil
	}
	return public
}

func workspaceChangeErrorMutated(changeErr *workspacechange.Error) bool {
	if changeErr == nil || changeErr.Details == nil {
		return false
	}
	mutated, _ := changeErr.Details["workspace_mutated"].(bool)
	return mutated
}

func workspaceChangeErrorRetryable(code string) bool {
	switch code {
	case workspacechange.ErrorCodeInvalidEdit,
		workspacechange.ErrorCodeRevisionConflict,
		workspacechange.ErrorCodeNotFound,
		workspacechange.ErrorCodeConflict,
		workspacechange.ErrorCodeDurabilityPending:
		return true
	default:
		return false
	}
}
