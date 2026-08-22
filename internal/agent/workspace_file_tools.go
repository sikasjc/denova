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

var workspaceReplaceLinesToolDescription = strings.TrimSpace(`Replace one or more non-overlapping complete line ranges in one workspace file as one reviewed change.
- Read the file first. Lines are 1-based and inclusive; copy that read_file revision verbatim into file_revision. Every replacement is resolved against that same original snapshot, so ranges never shift within this call.
- content is required. Use an empty string only to delete the selected lines. For an insertion, replace one adjacent line with the desired content plus that retained line.
- Batch all independent changes to this file in replacements. Re-read only when a revision conflict is returned or the current numbered body is unavailable; never recover by overwriting the whole file.`)

var workspaceReplaceTextToolDescription = strings.TrimSpace(`Replace every literal occurrence of one short, already-inspected text in one workspace file as a reviewed change.
- Use only for deliberate bulk renames or terminology changes, not prose rewriting. It applies to the file's current snapshot, so a read first is helpful but not required.
- find must be non-empty; replace is the new literal text. Every current literal occurrence is replaced in one reviewed change. The call may be attempted from context or a guessed term; if no occurrence exists, it returns an error without changing the file.
- For selected prose ranges use replace_lines; for a whole-file rewrite use write_file.`)

var workspaceWriteFileToolDescription = strings.TrimSpace(`Replace one workspace file completely as a reviewed change. Use replace_lines for localized changes and replace_text for guarded literal bulk changes; use write_file only for a new file or an explicitly requested full rewrite. A failed edit does not authorize overwriting an existing file.`)

type workspaceChangeService interface {
	Workspace() string
	ApplyEdits(context.Context, workspacechange.ApplyEditsRequest) (workspacechange.ChangeSet, error)
	ReplaceFile(context.Context, workspacechange.ReplaceFileRequest) (workspacechange.ChangeSet, error)
}

type workspaceReplaceLinesInput struct {
	FilePath     string                          `json:"file_path" jsonschema:"required,description=Workspace-relative or absolute file path"`
	FileRevision string                          `json:"file_revision" jsonschema:"required,description=Revision from the newest read_file or workspace change result; copy it verbatim"`
	Replacements []workspaceLineRangeReplacement `json:"replacements" jsonschema:"required,description=JSON array of non-overlapping complete-line replacements against one original snapshot"`
}

type workspaceLineRangeReplacement struct {
	StartLine int     `json:"start_line" jsonschema:"required,description=1-based first complete source line to replace"`
	EndLine   int     `json:"end_line,omitempty" jsonschema:"description=Inclusive last source line; defaults to start_line"`
	Content   *string `json:"content" jsonschema:"required,description=Replacement text; an explicit empty string deletes the selected lines"`
}

type workspaceReplaceTextInput struct {
	FilePath string  `json:"file_path" jsonschema:"required,description=Workspace-relative or absolute file path"`
	Find     string  `json:"find" jsonschema:"required,description=Non-empty literal text to replace exactly"`
	Replace  *string `json:"replace" jsonschema:"required,description=Replacement literal text; may be explicitly empty only when intentionally deleting every matched occurrence"`
}

type workspaceWriteFileInput struct {
	FilePath string `json:"file_path" jsonschema:"required,description=Workspace-relative or absolute file path"`
	Content  string `json:"content" jsonschema:"description=Complete replacement content"`
}

func newWorkspaceReplaceLinesTool(changes workspaceChangeService) (tool.BaseTool, error) {
	if changes == nil {
		return nil, fmt.Errorf("workspace change service is nil")
	}
	workspace, err := canonicalChangeWorkspace(changes)
	if err != nil {
		return nil, err
	}
	return utils.InferTool("replace_lines", workspaceReplaceLinesToolDescription, func(ctx context.Context, input workspaceReplaceLinesInput) (string, error) {
		baseRevision := strings.TrimSpace(input.FileRevision)
		logger := observability.Logger("agent-tool")
		logger.Info("replace_lines_called", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.Int("replacements", len(input.Replacements)), slog.Bool("model_file_revision", baseRevision != ""))
		if baseRevision == "" {
			logger.Warn("replace_lines_missing_file_revision", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.Int("replacements", len(input.Replacements)))
			return "", &workspacechange.Error{
				Code:    workspacechange.ErrorCodeInvalidEdit,
				Message: "缺少 file_revision：请从最近一次 read_file、replace_lines 或 write_file 结果中获取。",
				Details: map[string]any{
					"path":              input.FilePath,
					"field":             "file_revision",
					"workspace_mutated": false,
				},
			}
		}
		edits := make([]workspacechange.TextEdit, 0, len(input.Replacements))
		for index, replacement := range input.Replacements {
			if replacement.Content == nil {
				return "", &workspacechange.Error{
					Code:    workspacechange.ErrorCodeInvalidEdit,
					Message: "缺少 content：请显式传入替换文本；删除行时传 content 为 \"\"。",
					Details: map[string]any{
						"path":              input.FilePath,
						"field":             fmt.Sprintf("replacements[%d].content", index),
						"workspace_mutated": false,
					},
				}
			}
			edits = append(edits, workspacechange.TextEdit{
				StartLine: replacement.StartLine,
				EndLine:   replacement.EndLine,
				NewString: *replacement.Content,
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
			logger.Warn("replace_lines_failed", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.Int("replacements", len(input.Replacements)), slog.String("error_code", code), slog.String("expected_revision", expectedRevision), slog.String("actual_revision", actualRevision), slog.Any("error", err))
			return "", err
		}
		logger.Info("replace_lines_applied", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.Int("replacements", len(input.Replacements)), slog.String("change_set_id", changeSet.ID), slog.String("review_status", changeSet.ReviewStatus))
		rememberWorkspaceFileRevision(workspace, changeSet.Path, changeSet.Revision)
		return marshalWorkspaceChangeToolReceipt(workspace, changeSet)
	})
}

func newWorkspaceReplaceTextTool(changes workspaceChangeService) (tool.BaseTool, error) {
	if changes == nil {
		return nil, fmt.Errorf("workspace change service is nil")
	}
	workspace, err := canonicalChangeWorkspace(changes)
	if err != nil {
		return nil, err
	}
	return utils.InferTool("replace_text", workspaceReplaceTextToolDescription, func(ctx context.Context, input workspaceReplaceTextInput) (string, error) {
		logger := observability.Logger("agent-tool")
		logger.Info("replace_text_called", slog.String("workspace", workspace), slog.String("path", input.FilePath))
		if input.Replace == nil {
			return "", &workspacechange.Error{
				Code:    workspacechange.ErrorCodeInvalidEdit,
				Message: "缺少 replace：请显式传入替换文本；批量删除时传 replace 为 \"\"。",
				Details: map[string]any{
					"path":              input.FilePath,
					"field":             "replace",
					"workspace_mutated": false,
				},
			}
		}
		changeSet, err := changes.ApplyEdits(ctx, workspacechange.ApplyEditsRequest{
			Path: input.FilePath,
			Edits: []workspacechange.TextEdit{{
				ID:         "replace_text",
				OldString:  input.Find,
				NewString:  *input.Replace,
				ReplaceAll: true,
			}},
			Metadata: workspaceChangeMetadata(ctx),
		})
		if err != nil {
			code, expectedRevision, actualRevision := workspaceChangeErrorDiagnostics(err)
			logger.Warn("replace_text_failed", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.String("error_code", code), slog.String("expected_revision", expectedRevision), slog.String("actual_revision", actualRevision), slog.Any("error", err))
			return "", err
		}
		logger.Info("replace_text_applied", slog.String("workspace", workspace), slog.String("path", input.FilePath), slog.String("change_set_id", changeSet.ID), slog.String("review_status", changeSet.ReviewStatus))
		rememberWorkspaceFileRevision(workspace, changeSet.Path, changeSet.Revision)
		return marshalWorkspaceChangeToolReceipt(workspace, changeSet)
	})
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
	if changeErr == nil {
		return ""
	}
	if changeErr.Code != workspacechange.ErrorCodeRevisionConflict {
		return changeErr.Message
	}
	expected, actual := workspaceChangeErrorRevisions(changeErr)
	if expected == "" || actual == "" {
		return "Revision 冲突，请获取当前 revision 后重试。"
	}
	return fmt.Sprintf("Revision 冲突：传入 %s，当前 %s。", expected, actual)
}

// workspaceChangeErrorRevisions extracts the optimistic-concurrency pair from a
// change error's details.
func workspaceChangeErrorRevisions(changeErr *workspacechange.Error) (expected, actual string) {
	if changeErr == nil || changeErr.Details == nil {
		return "", ""
	}
	expectedValue, _ := changeErr.Details["expected_revision"].(string)
	actualValue, _ := changeErr.Details["actual_revision"].(string)
	return strings.TrimSpace(expectedValue), strings.TrimSpace(actualValue)
}

// workspaceChangeToolPublicErrorDetails forwards the structured details as-is.
// Revision values are deliberately included: they are content digests of the
// model's own workspace file, carry no secret, and are the only way for the
// model to tell a formatting mistake apart from a real concurrent write.
func workspaceChangeToolPublicErrorDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	public := make(map[string]any, len(details))
	for key, value := range details {
		public[key] = value
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

// workspaceChangeErrorDiagnostics extracts a workspacechange.Error's code plus
// the optimistic-concurrency pair for failure logs.
func workspaceChangeErrorDiagnostics(err error) (code, expectedRevision, actualRevision string) {
	var changeErr *workspacechange.Error
	if !errors.As(err, &changeErr) || changeErr == nil {
		return "", "", ""
	}
	expected, actual := workspaceChangeErrorRevisions(changeErr)
	return changeErr.Code, expected, actual
}
