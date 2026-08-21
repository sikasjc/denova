package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"

	"denova/internal/observability"
	"denova/internal/workspacechange"
)

var workspaceEditFileToolDescription = strings.TrimSpace(`Apply one or more line-based or exact text edits to a single workspace file as one reviewed change.
- file_path must identify one file inside the current workspace.
- Prefer start_line/end_line with new_string. read_file returns 1-based line numbers and a revision for this purpose; copy that revision to file_revision. If the numbered file body and its revision are already in your context, edit by line directly without re-reading; read the file first only when they are missing. A successful edit_file or write_file result returns the file's new revision plus each edit's before/after line ranges: pass the revision as file_revision to keep editing the same file without re-reading, and update line numbers for later edits from the reported ranges (a line after an edit's before-range moves by after_end_line - before_end_line). Re-read only when you cannot reliably compute the current line numbers (the file changed outside your own edits, or a revision conflict was reported). end_line is inclusive and defaults to start_line.
- A line edit replaces complete source lines. The tool preserves the file's line-ending style and keeps the following line separate when replacing a newline-terminated range. An empty new_string deletes the selected lines.
- Use old_string/new_string only when a line range cannot express the change: the change is inside one line (partial-line edit), or replace_all must replace every occurrence. Copy old_string verbatim from the latest read_file body, including punctuation, quotes, spaces and line breaks; never reconstruct it from memory.
- Every item in edits is resolved against the same original file snapshot, not against the result of an earlier item.
- Keep edits non-overlapping. Use replace_all only when every exact occurrence should change.
- Put dependent changes to the same file in one call. Independent files may use separate edit_file calls in the same assistant response.
- The tool captures and protects the current file snapshot internally when the call starts.
- Recover from failures without reflexively re-reading: if only a line range is out of bounds or old_string matching fails while the numbered body in your context is current (its metadata revision is the latest one you hold, possibly refreshed), fix the selector directly from that body — copy old_string verbatim from it — and retry. Read the file again only when a revision conflict is reported or the current numbered body is genuinely missing from your context; then rebuild the edit from the newest numbered output. Do not fall back to a full-file replacement.

将一个或多个按行或精确文本修改作为一次可审阅变更应用到同一个 workspace 文件。
- file_path 必须指向当前 workspace 内的单个文件。
- 优先使用 start_line/end_line 和 new_string。read_file 会为此返回从 1 开始的行号与 revision，请把该 revision 传入 file_revision。上下文中已有带编号的文件正文及其 revision 时直接按行修改、无需重读；两者缺失时才先 read_file。edit_file / write_file 成功后会返回文件的新 revision 以及每个修改的前后行号区间：继续修改同一文件时把该 revision 作为 file_revision 传入即可免重读，并利用返回的行号区间推算后续行号（位于某个修改 before 区间之后的行，按 after_end_line - before_end_line 平移）。只有无法可靠推算当前行号（文件在你的修改之外发生变化、或返回了 revision 冲突）时才重新 read_file。end_line 包含在替换范围内，省略时等于 start_line。
- 按行修改会替换完整源文件行，工具会保持文件换行符风格；被替换范围原本带换行符时会保持下一行独立。new_string 为空表示删除所选行。
- 仅当修改无法用行范围表达时才使用 old_string/new_string：修改发生在同一行内部（行内局部修改），或需要 replace_all 替换所有出现。old_string 必须从最新 read_file 结果逐字复制，保留标点、引号、空格和换行，禁止凭记忆重建。
- edits 中的每一项都基于同一份原始文件快照解析，不基于前一项修改后的结果。
- 各修改区间不得重叠；只有确实需要替换全部精确匹配时才使用 replace_all。
- 同一文件内相互依赖的修改必须放在一次调用中；不同文件的独立修改可以在同一轮分别调用 edit_file。
- 工具会在调用开始时自行获取并保护当前文件快照。
- 从失败中恢复时不要条件反射式地重读：如果只是行号越界或 old_string 匹配失败，而上下文中的带行号正文仍是当前的（其元数据 revision 是你持有的最新值，可能是 refreshed 的），直接从该正文修正选择器——old_string 从中逐字复制——后重试。只有返回 revision 冲突、或上下文中确实缺少当前带行号正文时才重新 read_file，并按最新带行号输出重建修改。不要降级为整文件覆盖。`)

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
	StartLine  int    `json:"start_line,omitempty" jsonschema:"description=Primary selector: 1-based first complete source line to replace; use whenever numbered file content from read_file is in context"`
	EndLine    int    `json:"end_line,omitempty" jsonschema:"description=Inclusive 1-based last source line; defaults to start_line"`
	OldString  string `json:"old_string,omitempty" jsonschema:"description=Only when a line range cannot express the change: partial-line edits inside one line, or with replace_all; exact non-empty text copied verbatim from the read_file body; mutually exclusive with start_line/end_line"`
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
		lineEdits, exactEdits := 0, 0
		for _, edit := range input.Edits {
			if edit.StartLine != 0 || edit.EndLine != 0 {
				lineEdits++
			} else {
				exactEdits++
			}
		}
		logger := observability.Logger("agent-tool")
		logger.Info("edit_file_called", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.Int("edits", len(input.Edits)), slog.Int("line_edits", lineEdits), slog.Int("exact_edits", exactEdits), slog.Bool("model_file_revision", baseRevision != ""))
		if containsLineBasedEdit(input.Edits) && baseRevision == "" {
			logger.Warn("edit_file_missing_file_revision", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.Int("line_edits", lineEdits))
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
		// Exact-only edits may omit file_revision: the service resolves the base
		// from the current snapshot under its own lock, and old_string matching
		// is the freshness anchor. Skipping the pre-read removes one full
		// read + hash per call.
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
			code, expectedRevision, actualRevision := workspaceChangeErrorDiagnostics(err)
			logger.Warn("edit_file_failed", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.Int("line_edits", lineEdits), slog.Int("exact_edits", exactEdits), slog.String("error_code", code), slog.String("expected_revision", expectedRevision), slog.String("actual_revision", actualRevision), slog.Any("error", err))
			return "", err
		}
		logger.Info("edit_file_applied", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.Int("line_edits", lineEdits), slog.Int("exact_edits", exactEdits), slog.String("change_set_id", changeSet.ID), slog.String("review_status", changeSet.ReviewStatus))
		rememberWorkspaceFileRevision(workspace, changeSet.Path, changeSet.Revision)
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
		logger := observability.Logger("agent-tool")
		logger.Info("write_file_called", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.Int("content_bytes", len(input.Content)))
		// An empty base revision lets the service anchor against the current
		// state under its mutation lock, which is write_file's intent: it never
		// read the file and always means to replace what is there now.
		changeSet, err := changes.ReplaceFile(ctx, workspacechange.ReplaceFileRequest{
			Path:         input.FilePath,
			Content:      input.Content,
			BaseRevision: "",
			Metadata:     workspaceChangeMetadata(ctx),
		})
		if err != nil {
			logger.Warn("write_file_failed", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.Any("error", err))
			return "", err
		}
		rememberWorkspaceFileRevision(workspace, changeSet.Path, changeSet.Revision)
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
		Edits:          workspaceChangeEditReceipts(changeSet),
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return "", fmt.Errorf("serialize workspace change receipt: %w", err)
	}
	return string(data), nil
}

// workspaceChangeReceiptMaxEdits bounds the per-edit line metadata in a tool
// receipt. Line hints exist to let the model chain a handful of edits without
// re-reading; very large batches must re-read anyway, and the receipt has to
// stay small no matter how many edits one call carries.
const workspaceChangeReceiptMaxEdits = 20

// workspaceChangeEditReceipts projects each applied edit's hunks into compact
// before/after line ranges so the model can shift line numbers for chained
// edits without re-reading the file. Entries beyond
// workspaceChangeReceiptMaxEdits are dropped to keep the receipt bounded.
func workspaceChangeEditReceipts(changeSet workspacechange.ChangeSet) []workspaceChangeEditReceipt {
	if len(changeSet.Edits) == 0 || len(changeSet.Edits) > workspaceChangeReceiptMaxEdits {
		return nil
	}
	receipts := make([]workspaceChangeEditReceipt, 0, len(changeSet.Edits))
	for _, edit := range changeSet.Edits {
		if len(edit.Hunks) == 0 {
			continue
		}
		hunks := make([]workspaceChangeHunkReceipt, 0, len(edit.Hunks))
		for _, hunk := range edit.Hunks {
			if hunk.BeforeStartLine == 0 && hunk.BeforeEndLine == 0 && hunk.AfterStartLine == 0 && hunk.AfterEndLine == 0 {
				continue
			}
			hunks = append(hunks, workspaceChangeHunkReceipt{
				BeforeStartLine: hunk.BeforeStartLine,
				BeforeEndLine:   hunk.BeforeEndLine,
				AfterStartLine:  hunk.AfterStartLine,
				AfterEndLine:    hunk.AfterEndLine,
			})
		}
		if len(hunks) == 0 {
			continue
		}
		receipts = append(receipts, workspaceChangeEditReceipt{
			ID:           edit.ID,
			Replacements: len(edit.Hunks),
			Hunks:        hunks,
		})
	}
	if len(receipts) == 0 {
		return nil
	}
	return receipts
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
		return "Workspace file changed after your read or during the call (possibly by your own earlier edit). Re-read the file and rebuild the edit from current numbered output, then copy the revision from that newest read_file result. Do not reuse a file_revision value from an earlier attempt or an older context snapshot. / 文件在读取后或调用期间发生了变化（可能是你自己之前的修改）。请重新读取文件并按最新带行号内容重建修改，然后使用这次最新 read_file 结果里的 revision；不要复用之前尝试或旧上下文快照里的 file_revision。"
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

// workspaceChangeErrorDiagnostics 提取 workspacechange.Error 的错误码与乐观并发
// 冲突细节（模型提交的 expected_revision 与磁盘当前 actual_revision）。
// 仅用于失败日志排查 revision 冲突，不会进入模型可见的工具结果。
func workspaceChangeErrorDiagnostics(err error) (code, expectedRevision, actualRevision string) {
	var changeErr *workspacechange.Error
	if !errors.As(err, &changeErr) || changeErr == nil {
		return "", "", ""
	}
	code = changeErr.Code
	if details := changeErr.Details; details != nil {
		expected, _ := details["expected_revision"].(string)
		actual, _ := details["actual_revision"].(string)
		expectedRevision = strings.TrimSpace(expected)
		actualRevision = strings.TrimSpace(actual)
	}
	return code, expectedRevision, actualRevision
}
