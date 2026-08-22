package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"denova/internal/workspacechange"
)

type recordingWorkspaceChangeService struct {
	workspace      string
	readCalls      int
	readPath       string
	readRevision   string
	readErr        error
	applyCalls     int
	replaceCalls   int
	applyRequest   workspacechange.ApplyEditsRequest
	replaceRequest workspacechange.ReplaceFileRequest
	changeSet      workspacechange.ChangeSet
	err            error
}

func (s *recordingWorkspaceChangeService) Workspace() string {
	return s.workspace
}

func (s *recordingWorkspaceChangeService) ReadFile(path string) (string, string, error) {
	s.readCalls++
	s.readPath = path
	if s.readErr != nil {
		return "", "", s.readErr
	}
	revision := s.readRevision
	if revision == "" {
		revision = "sha256:current"
	}
	return "", revision, nil
}

func (s *recordingWorkspaceChangeService) ApplyEdits(_ context.Context, request workspacechange.ApplyEditsRequest) (workspacechange.ChangeSet, error) {
	s.applyCalls++
	s.applyRequest = request
	return s.changeSet, s.err
}

func (s *recordingWorkspaceChangeService) ReplaceFile(_ context.Context, request workspacechange.ReplaceFileRequest) (workspacechange.ChangeSet, error) {
	s.replaceCalls++
	s.replaceRequest = request
	return s.changeSet, s.err
}

func TestWorkspaceReplaceLinesToolBatchesOneFileAndReturnsBoundedReceipt(t *testing.T) {
	service := &recordingWorkspaceChangeService{workspace: t.TempDir(), readRevision: "sha256:before", changeSet: workspacechange.ChangeSet{
		ID:            "change-1",
		GroupID:       "run-1",
		Path:          "chapters/ch01.md",
		BaseRevision:  "sha256:before",
		Revision:      "sha256:after",
		BeforeContent: strings.Repeat("before", 100),
		AfterContent:  strings.Repeat("after", 100),
		ReviewStatus:  workspacechange.ReviewStatusPending,
		ApplyState:    workspacechange.ApplyStateApplied,
		Edits: []workspacechange.AppliedEdit{
			{ID: "opening", Hunks: []workspacechange.Hunk{{ID: "h1"}}},
			{ID: "ending", Hunks: []workspacechange.Hunk{{ID: "h2"}, {ID: "h3"}}},
		},
	}}
	for index := 0; index < 10_000; index++ {
		service.changeSet.Edits = append(service.changeSet.Edits, workspacechange.AppliedEdit{
			ID:    fmt.Sprintf("bulk-%d", index),
			Hunks: []workspacechange.Hunk{{ID: fmt.Sprintf("bulk-hunk-%d", index)}},
		})
	}
	base, err := newWorkspaceReplaceLinesTool(service)
	if err != nil {
		t.Fatal(err)
	}
	invokable, ok := base.(tool.InvokableTool)
	if !ok {
		t.Fatal("replace_lines should be invokable")
	}
	observer := newRunObserver(&RunLedger{id: "run-1"}, "")
	ctx := ContextWithRunObserver(context.Background(), observer)
	result, err := invokable.InvokableRun(ctx, `{
        "file_path":"chapters/ch01.md",
        "file_revision":"sha256:before",
        "replacements":[
          {"start_line":12,"end_line":14,"content":"new 1"},
          {"start_line":20,"content":"new 2"}
        ]
      }`)
	if err != nil {
		t.Fatal(err)
	}
	if service.readCalls != 0 || service.applyRequest.Path != "chapters/ch01.md" || service.applyRequest.BaseRevision != "sha256:before" {
		t.Fatalf("unexpected request: %#v", service.applyRequest)
	}
	if len(service.applyRequest.Edits) != 2 ||
		service.applyRequest.Edits[0].StartLine != 12 ||
		service.applyRequest.Edits[0].EndLine != 14 ||
		service.applyRequest.Edits[1].StartLine != 20 ||
		service.applyRequest.Edits[1].NewString != "new 2" {
		t.Fatalf("batch edits were not preserved: %#v", service.applyRequest.Edits)
	}
	if service.applyRequest.Metadata.Origin != workspacechange.OriginAgent ||
		service.applyRequest.Metadata.RunID != "run-1" ||
		service.applyRequest.Metadata.ChangeGroupID != "run-1" {
		t.Fatalf("unexpected metadata: %#v", service.applyRequest.Metadata)
	}
	var receipt workspaceChangeToolReceipt
	if err := json.Unmarshal([]byte(result), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Schema != workspaceChangeToolResultSchema || receipt.Workspace != service.workspace || receipt.ChangeSetID != "change-1" || len(receipt.Edits) != 0 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	if strings.Contains(result, `"edits"`) || len(result) > 4096 {
		t.Fatalf("receipt grew with per-edit details: bytes=%d result=%s", len(result), result)
	}
	if strings.Contains(result, "beforebefore") || strings.Contains(result, "afterafter") {
		t.Fatalf("receipt leaked file content: %s", result)
	}
}

func TestWorkspaceFileToolDescriptionsStayCompactAndPreserveSafetyRules(t *testing.T) {
	if len(workspaceReplaceLinesToolDescription) > 1200 {
		t.Fatalf("replace_lines description is too large: %d bytes", len(workspaceReplaceLinesToolDescription))
	}
	for _, expected := range []string{
		"Read the file first",
		"file_revision",
		"content is required",
		"same original snapshot",
		"never recover by overwriting the whole file",
	} {
		if !strings.Contains(workspaceReplaceLinesToolDescription, expected) {
			t.Fatalf("replace_lines description missing %q:\n%s", expected, workspaceReplaceLinesToolDescription)
		}
	}
	if len(workspaceReplaceTextToolDescription) > 1000 || !strings.Contains(workspaceReplaceTextToolDescription, "not required") {
		t.Fatalf("replace_text description is not compact and safe: %s", workspaceReplaceTextToolDescription)
	}
	if len(workspaceWriteFileToolDescription) > 500 || !strings.Contains(workspaceWriteFileToolDescription, "failed edit does not authorize overwriting") {
		t.Fatalf("write_file description is not compact and safe: %s", workspaceWriteFileToolDescription)
	}
}

func TestWorkspaceChangeMetadataUsesStableRunIdentityWithoutLedger(t *testing.T) {
	observer := newRunObserverWithIdentity(nil, "", "task-run", "session-1", "review-thread-1")
	metadata := workspaceChangeMetadata(ContextWithRunObserver(context.Background(), observer))

	if metadata.ChangeGroupID != "task-run" || metadata.RunID != "task-run" {
		t.Fatalf("stable run identity was lost without a ledger: %#v", metadata)
	}
	if metadata.SessionID != "session-1" || metadata.ReviewThreadID != "review-thread-1" {
		t.Fatalf("review linkage metadata was lost: %#v", metadata)
	}
}

func TestWorkspaceReplaceLinesToolPublishesBatchSchema(t *testing.T) {
	base, err := newWorkspaceReplaceLinesTool(&recordingWorkspaceChangeService{workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	info, err := base.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var encoded struct {
		JSONSchema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"json_schema"`
	}
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"file_path", "file_revision", "replacements"} {
		if _, ok := encoded.JSONSchema.Properties[name]; !ok {
			t.Fatalf("batch edit schema is missing root property %q: %s", name, data)
		}
	}
	if _, ok := encoded.JSONSchema.Properties["base_revision"]; ok || containsStringValue(encoded.JSONSchema.Required, "base_revision") {
		t.Fatalf("batch edit schema exposes internal base_revision: %s", data)
	}
	for _, legacy := range []string{"old_string", "new_string", "replace_all"} {
		if _, ok := encoded.JSONSchema.Properties[legacy]; ok {
			t.Fatalf("legacy single-edit property %q remains at schema root: %s", legacy, data)
		}
	}
	var replacementsProperty struct {
		Items struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"items"`
	}
	if err := json.Unmarshal(encoded.JSONSchema.Properties["replacements"], &replacementsProperty); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"start_line", "end_line", "content"} {
		if _, ok := replacementsProperty.Items.Properties[name]; !ok {
			t.Fatalf("replacement item schema is missing %q: %s", name, data)
		}
	}
	for _, legacy := range []string{"old_string", "new_string", "replace_all"} {
		if _, ok := replacementsProperty.Items.Properties[legacy]; ok {
			t.Fatalf("legacy replacement property %q remains visible: %s", legacy, data)
		}
	}
	if !containsStringValue(replacementsProperty.Items.Required, "content") {
		t.Fatalf("content must be required so omission cannot delete lines: %s", data)
	}
}

func TestWorkspaceReplaceLinesRequiresAndForwardsReadRevision(t *testing.T) {
	service := &recordingWorkspaceChangeService{workspace: t.TempDir(), readRevision: "sha256:current"}
	base, err := newWorkspaceReplaceLinesTool(service)
	if err != nil {
		t.Fatal(err)
	}
	invokable := base.(tool.InvokableTool)
	_, err = invokable.InvokableRun(context.Background(), `{
		"file_path":"chapters/ch01.md",
		"replacements":[{"start_line":2,"content":"replacement"}]
	}`)
	if err == nil || !strings.Contains(err.Error(), "file_revision") {
		t.Fatalf("line edit without read revision should fail, got %v", err)
	}
	if service.readCalls != 0 || service.applyCalls != 0 {
		t.Fatalf("missing line revision should fail before workspace access: %#v", service)
	}

	_, err = invokable.InvokableRun(context.Background(), `{
		"file_path":"chapters/ch01.md",
		"file_revision":"sha256:read-snapshot",
		"replacements":[{"start_line":2,"end_line":3,"content":"replacement"}]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if service.readCalls != 0 || service.applyRequest.BaseRevision != "sha256:read-snapshot" {
		t.Fatalf("line edit did not forward read revision atomically: %#v", service.applyRequest)
	}
}

func TestWorkspaceReplaceLinesTrimsRevisionWhitespaceOnly(t *testing.T) {
	service := &recordingWorkspaceChangeService{workspace: t.TempDir()}
	base, err := newWorkspaceReplaceLinesTool(service)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{
		"file_path":"chapters/ch01.md",
		"file_revision":"  abcdef12  ",
		"replacements":[{"start_line":2,"content":"replacement"}]
	}`
	if _, err := base.(tool.InvokableTool).InvokableRun(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if service.applyRequest.BaseRevision != "abcdef12" {
		t.Fatalf("revision = %q, want surrounding whitespace trimmed", service.applyRequest.BaseRevision)
	}
}

func TestWorkspaceReplaceLinesRejectsMissingContent(t *testing.T) {
	service := &recordingWorkspaceChangeService{workspace: t.TempDir()}
	base, err := newWorkspaceReplaceLinesTool(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = base.(tool.InvokableTool).InvokableRun(context.Background(), `{
		"file_path":"chapters/ch01.md",
		"file_revision":"sha256:before",
		"replacements":[{"start_line":2}]
	}`)
	if err == nil || !strings.Contains(err.Error(), "content") {
		t.Fatalf("missing content should fail safely, got %v", err)
	}
	if service.applyCalls != 0 {
		t.Fatalf("missing content must not mutate: %#v", service.applyRequest)
	}
}

func TestWorkspaceReplaceTextToolForwardsLiteralReplacement(t *testing.T) {
	service := &recordingWorkspaceChangeService{workspace: t.TempDir(), changeSet: workspacechange.ChangeSet{
		ID: "change-text", Path: "chapters/ch01.md", Revision: "sha256:after", ReviewStatus: workspacechange.ReviewStatusPending, ApplyState: workspacechange.ApplyStateApplied,
	}}
	base, err := newWorkspaceReplaceTextTool(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = base.(tool.InvokableTool).InvokableRun(context.Background(), `{
		"file_path":"chapters/ch01.md",
		"find":"林晚",
		"replace":"林晚晴"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	request := service.applyRequest
	if service.applyCalls != 1 || request.BaseRevision != "" || len(request.Edits) != 1 || request.Edits[0].OldString != "林晚" || request.Edits[0].NewString != "林晚晴" || !request.Edits[0].ReplaceAll {
		t.Fatalf("replace_text did not forward a direct literal replacement: %#v", request)
	}
}

func TestWorkspaceReplaceTextRejectsMissingReplacement(t *testing.T) {
	service := &recordingWorkspaceChangeService{workspace: t.TempDir()}
	base, err := newWorkspaceReplaceTextTool(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = base.(tool.InvokableTool).InvokableRun(context.Background(), `{
		"file_path":"chapters/ch01.md",
		"file_revision":"sha256:before",
		"find":"林晚"
	}`)
	if err == nil || !strings.Contains(err.Error(), "replace") {
		t.Fatalf("missing replacement should fail safely, got %v", err)
	}
	if service.applyCalls != 0 {
		t.Fatalf("missing replacement must not mutate: %#v", service.applyRequest)
	}
}

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestWorkspaceWriteFileToolUsesChangeService(t *testing.T) {
	service := &recordingWorkspaceChangeService{workspace: t.TempDir(), readRevision: "sha256:before", changeSet: workspacechange.ChangeSet{
		ID:           "change-write",
		GroupID:      "group-write",
		Path:         "ideas.md",
		BaseRevision: "sha256:before",
		Revision:     "sha256:after",
		ReviewStatus: workspacechange.ReviewStatusPending,
		ApplyState:   workspacechange.ApplyStateApplied,
	}}
	base, err := newWorkspaceWriteFileTool(service)
	if err != nil {
		t.Fatal(err)
	}
	result, err := base.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"ideas.md","content":"new"}`)
	if err != nil {
		t.Fatal(err)
	}
	// write_file never reads the file; the service anchors the replacement
	// against the state it observes under its own mutation lock.
	if service.readCalls != 0 || service.replaceRequest.Path != "ideas.md" || service.replaceRequest.Content != "new" || service.replaceRequest.BaseRevision != "" {
		t.Fatalf("unexpected replace request: %#v", service.replaceRequest)
	}
	if !strings.Contains(result, `"change_set_id":"change-write"`) {
		t.Fatalf("unexpected write receipt: %s", result)
	}
}

func TestWorkspaceWriteFileToolHidesBaseRevisionFromSchema(t *testing.T) {
	base, err := newWorkspaceWriteFileTool(&recordingWorkspaceChangeService{workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	info, err := base.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var encoded struct {
		JSONSchema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"json_schema"`
	}
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := encoded.JSONSchema.Properties["base_revision"]; ok || containsStringValue(encoded.JSONSchema.Required, "base_revision") {
		t.Fatalf("write schema exposes internal base_revision: %s", data)
	}
}

func TestWorkspaceReplaceLinesLeavesFileUntouchedWhenOneBatchEditFails(t *testing.T) {
	workspace := t.TempDir()
	chapterDir := filepath.Join(workspace, "chapters")
	if err := os.MkdirAll(chapterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chapterDir, "ch01.md")
	original := "opening\n\nmiddle\n\nending"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := workspacechange.NewService(workspace)
	if err != nil {
		t.Fatal(err)
	}
	base, err := newWorkspaceReplaceLinesTool(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = base.(tool.InvokableTool).InvokableRun(context.Background(), `{
	        "file_path":"chapters/ch01.md",
	        "file_revision":"`+workspacechange.Revision([]byte(original))+`",
	        "replacements":[
          {"start_line":1,"content":"new opening"},
          {"start_line":100,"content":"replacement"}
        ]
      }`)
	if err == nil {
		t.Fatal("batch with an invalid line range should fail")
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != original {
		t.Fatalf("failed batch changed file: %q", content)
	}
	groups, listErr := service.ListGroups(context.Background(), workspacechange.ChangeFilter{})
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(groups) != 0 {
		t.Fatalf("failed batch created review history: %#v", groups)
	}
}

func TestWorkspaceChangeErrorBecomesStructuredToolError(t *testing.T) {
	err := &workspacechange.Error{
		Code:    workspacechange.ErrorCodeInvalidEdit,
		Message: "old_string appears more than once",
		Details: map[string]any{"edit_index": 1, "match_count": 2},
	}
	message, ok := formatWorkspaceChangeToolError("edit_file", err)
	if !ok {
		t.Fatal("workspace change error should be recognized")
	}
	if !strings.HasPrefix(message, "[tool error]\n") ||
		!strings.Contains(message, `"code":"invalid_edit"`) ||
		!strings.Contains(message, `"workspace_mutated":false`) ||
		!strings.Contains(message, `"retryable":true`) {
		t.Fatalf("unexpected structured error: %s", message)
	}
}

func TestDurabilityPendingToolErrorIsRetryableAndReportsVisibleMutation(t *testing.T) {
	err := &workspacechange.Error{
		Code:    workspacechange.ErrorCodeDurabilityPending,
		Message: "workspace mutation durability is pending",
		Details: map[string]any{"path": "chapters/ch01.md", "workspace_mutated": true},
	}
	message, ok := formatWorkspaceChangeToolError("edit_file", err)
	if !ok ||
		!strings.Contains(message, `"code":"durability_pending"`) ||
		!strings.Contains(message, `"workspace_mutated":true`) ||
		!strings.Contains(message, `"retryable":true`) {
		t.Fatalf("unexpected durability receipt: %s", message)
	}
}

func TestWorkspaceReplaceLinesRejectsEmptyRevisionBeforeWorkspaceAccess(t *testing.T) {
	service := &recordingWorkspaceChangeService{workspace: t.TempDir(), readRevision: "sha256:current"}
	replaceLines, err := newWorkspaceReplaceLinesTool(service)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replaceLines.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"ideas.md","replacements":[{"start_line":1,"content":"b"}]}`); err == nil {
		t.Fatal("replace_lines without a revision should fail")
	}
	if service.applyCalls != 0 || service.readCalls != 0 {
		t.Fatalf("missing revision should fail before workspace access: %#v", service.applyRequest)
	}

	write, err := newWorkspaceWriteFileTool(service)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"ideas.md","content":"new"}`); err != nil {
		t.Fatal(err)
	}
	if service.replaceCalls != 1 || service.replaceRequest.BaseRevision != "" || service.readCalls != 0 {
		t.Fatalf("write_file should pass an empty revision without pre-reading: %#v", service.replaceRequest)
	}
}

func TestWorkspaceWriteFileToolDelegatesMissingDetectionToService(t *testing.T) {
	service := &recordingWorkspaceChangeService{workspace: t.TempDir(), readErr: &workspacechange.Error{
		Code: workspacechange.ErrorCodeNotFound, Message: "workspace file not found",
	}, changeSet: workspacechange.ChangeSet{
		ID: "created", GroupID: "group", Path: "new.md", BaseRevision: "missing", Revision: "sha256:after",
	}}
	write, err := newWorkspaceWriteFileTool(service)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"new.md","content":"new"}`); err != nil {
		t.Fatal(err)
	}
	// Missing-file detection now happens inside ReplaceFile against the state
	// observed under the mutation lock, so the tool never reads the file.
	if service.replaceCalls != 1 || service.replaceRequest.BaseRevision != "" || service.readCalls != 0 {
		t.Fatalf("create should delegate missing detection to the service: %#v", service.replaceRequest)
	}
}

// A revision conflict is only actionable if the model can see which revision it
// sent and which one the file is at: without both, a model that merely mangled
// the token concludes the file was rewritten and re-reads forever. Revisions are
// content digests of the model's own workspace file and carry no secret, so they
// are reported in full.
func TestWorkspaceChangeConflictErrorNamesBothRevisions(t *testing.T) {
	err := &workspacechange.Error{
		Code:    workspacechange.ErrorCodeRevisionConflict,
		Message: "workspace file revision changed",
		Details: map[string]any{
			"path":              "chapters/ch01.md",
			"expected_revision": "aaaa1111",
			"actual_revision":   "bbbb2222",
		},
	}
	message, ok := formatWorkspaceChangeToolError("edit_file", err)
	if !ok {
		t.Fatal("workspace change error should be recognized")
	}
	for _, want := range []string{"aaaa1111", "bbbb2222", "expected_revision", "actual_revision"} {
		if !strings.Contains(message, want) {
			t.Fatalf("conflict error should report %q: %s", want, message)
		}
	}
}

func TestWorkspaceChangeReceiptReturnsNewRevisionAndHidesBaseRevision(t *testing.T) {
	raw := `{"schema":"workspace_change.tool_result.v1","status":"applied","workspace":"/workspace/book-a","change_group_id":"group-1","change_set_id":"change-1","path":"chapters/ch01.md","base_revision":"sha256:before","revision":"sha256:after","review_status":"pending","apply_state":"applied"}`
	filtered := FilterToolResultForModel("replace_lines", `{"file_path":"chapters/ch01.md","replacements":[]}`, raw)
	if strings.Contains(filtered.Content, "change_set_id") || strings.Contains(filtered.Content, "apply_state") || strings.Contains(filtered.Content, `"status"`) {
		t.Fatalf("model receipt retained workflow metadata: %s", filtered.Content)
	}
	if !strings.Contains(filtered.Content, `"path":"chapters/ch01.md"`) {
		t.Fatalf("model receipt lost the edited path: %s", filtered.Content)
	}
	if !strings.Contains(filtered.Content, `"revision":"sha256:after"`) {
		t.Fatalf("model receipt did not return the new revision for chaining: %s", filtered.Content)
	}
	if strings.Contains(filtered.Content, "base_revision") || strings.Contains(filtered.Content, "sha256:before") {
		t.Fatalf("model receipt exposed the pre-edit base revision: %s", filtered.Content)
	}
}

func TestMutationTrackerAssociatesWorkspaceChangeReceipt(t *testing.T) {
	tracker := newMutationTracker()
	tracker.Observe(Event{Type: "tool_call", Data: map[string]any{
		"id":   "call-1",
		"name": "replace_lines",
		"args": `{"file_path":"chapters/ch01.md","replacements":[]}`,
	}})
	tracker.Observe(Event{Type: "tool_result", Data: map[string]any{
		"id":      "call-1",
		"name":    "replace_lines",
		"content": `{"schema":"workspace_change.tool_result.v1","status":"applied","workspace":"/workspace/book-a","change_group_id":"group-1","change_set_id":"change-1","path":"chapters/ch01.md","base_revision":"sha256:before","revision":"sha256:after","review_status":"pending","apply_state":"applied"}`,
	}})
	mutations := tracker.Mutations()
	if len(mutations) != 1 {
		t.Fatalf("mutations = %#v", mutations)
	}
	mutation := mutations[0]
	if mutation.Workspace != "/workspace/book-a" || mutation.ChangeGroupID != "group-1" || mutation.ChangeSetID != "change-1" || mutation.Revision != "sha256:after" || mutation.Target != "chapters/ch01.md" {
		t.Fatalf("workspace change identity was not tracked: %#v", mutation)
	}
}

func TestWorkspaceChangeReceiptIsTrustedOnlyForWorkspaceFileTools(t *testing.T) {
	content := `{"schema":"workspace_change.tool_result.v1","status":"applied","workspace":"/workspace/book-a","change_group_id":"group-1","change_set_id":"change-1","path":"chapters/ch01.md","base_revision":"sha256:before","revision":"sha256:after","review_status":"pending","apply_state":"applied"}`
	for _, toolName := range []string{"read_file", "execute", "grep"} {
		if _, ok := parseWorkspaceChangeToolReceipt(toolName, content); ok {
			t.Fatalf("untrusted tool %q forged a workspace change receipt", toolName)
		}
	}
	receipt, ok := parseWorkspaceChangeToolReceipt("replace_lines", content)
	if !ok || receipt.Workspace != "/workspace/book-a" || receipt.ChangeSetID != "change-1" {
		t.Fatalf("trusted receipt was not parsed: %#v ok=%t", receipt, ok)
	}
	record := ToolExecutionRecord{ToolName: "write_file"}
	applyWorkspaceChangeReceiptToExecutionRecord(&record, content)
	if record.Workspace != "/workspace/book-a" || record.ChangeSetID != "change-1" {
		t.Fatalf("execution record lost workspace identity: %#v", record)
	}
	forged := ToolExecutionRecord{ToolName: "read_file"}
	applyWorkspaceChangeReceiptToExecutionRecord(&forged, content)
	if forged.Workspace != "" || forged.ChangeSetID != "" {
		t.Fatalf("read_file forged an execution record receipt: %#v", forged)
	}
}

func TestWorkspaceChangeConflictErrorIsCompactChinese(t *testing.T) {
	err := &workspacechange.Error{
		Code:    workspacechange.ErrorCodeRevisionConflict,
		Message: "workspace file revision changed",
		Details: map[string]any{
			"path":              "chapters/ch01.md",
			"expected_revision": "aaaa1111",
			"actual_revision":   "bbbb2222",
		},
	}
	message, ok := formatWorkspaceChangeToolError("edit_file", err)
	if !ok {
		t.Fatal("workspace change error should be recognized")
	}
	for _, expected := range []string{`"code":"revision_conflict"`, "Revision 冲突：传入 aaaa1111，当前 bbbb2222。"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("conflict message missing %q:\n%s", expected, message)
		}
	}
	if strings.Contains(message, "Copy") || strings.Contains(message, "re-read") {
		t.Fatalf("conflict message should stay compact: %s", message)
	}
}

func TestWorkspaceChangeErrorDiagnosticsExtractConflictRevisions(t *testing.T) {
	err := &workspacechange.Error{
		Code:    workspacechange.ErrorCodeRevisionConflict,
		Message: "workspace file revision changed",
		Details: map[string]any{
			"expected_revision": " sha256:before ",
			"actual_revision":   "sha256:after",
			"path":              "chapters/ch01.md",
		},
	}
	code, expected, actual := workspaceChangeErrorDiagnostics(err)
	if code != workspacechange.ErrorCodeRevisionConflict || expected != "sha256:before" || actual != "sha256:after" {
		t.Fatalf("diagnostics = %q %q %q", code, expected, actual)
	}
	if code, expected, actual := workspaceChangeErrorDiagnostics(fmt.Errorf("plain failure")); code != "" || expected != "" || actual != "" {
		t.Fatalf("non-workspacechange error leaked diagnostics: %q %q %q", code, expected, actual)
	}
}
