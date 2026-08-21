package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"denova/internal/workspacechange"
)

// TestCrossTurnAssemblyRefreshesEditedFileBody is the end-to-end anti re-read
// guarantee: after the model reads a file and edits it, the next turn's context
// assembly refreshes that read body to the file's CURRENT content with a
// current revision and the refreshed marker — the model never needs to re-read
// what it just edited.
func TestCrossTurnAssemblyRefreshesEditedFileBody(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "chapters", "ch01.md")
	const original = "第一行\n第二行\n第三行\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := workspacechange.NewService(workspace)
	if err != nil {
		t.Fatal(err)
	}
	readTool, err := newWorkspaceReadFileTool(newTestAgentFilesystemBackend(t, workspace), workspace)
	if err != nil {
		t.Fatal(err)
	}
	editTool, err := newWorkspaceEditFileTool(service)
	if err != nil {
		t.Fatal(err)
	}

	// Turn 1: read the file, remember the stored tool result verbatim.
	readResult, err := readTool.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"`+path+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var readMetadata workspaceReadFileMetadata
	metadataLine, _, _ := strings.Cut(readResult, "\n")
	if err := json.Unmarshal([]byte(metadataLine), &readMetadata); err != nil {
		t.Fatal(err)
	}
	if readMetadata.Refreshed {
		t.Fatalf("a live read_file result must never be marked refreshed: %s", metadataLine)
	}

	// The model edits line 2, changing the file the read anchored to.
	if _, err := editTool.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"`+path+`","file_revision":"`+readMetadata.Revision+`","edits":[{"start_line":2,"new_string":"改写后的第二行\n新增的一行"}]}`); err != nil {
		t.Fatal(err)
	}

	// Turn 2 assembly over the stored history must refresh, not collapse.
	messages := readFileExchange("call-1", path, readMetadata.Revision, "第一行\n第二行\n第三行")
	resolver := NewWorkspaceFileResolver(workspace)
	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), resolver)
	if len(filtered) != 2 {
		t.Fatalf("refreshed pair must survive assembly: %#v", filtered)
	}
	content := filtered[1].Content
	if strings.Contains(content, retainedToolReceiptSchema) {
		t.Fatalf("edited file must refresh its body instead of collapsing: %s", content)
	}
	var refreshedMetadata workspaceReadFileMetadata
	refreshedLine, _, _ := strings.Cut(content, "\n")
	if err := json.Unmarshal([]byte(refreshedLine), &refreshedMetadata); err != nil {
		t.Fatal(err)
	}
	if !refreshedMetadata.Refreshed {
		t.Fatalf("refreshed body must carry the refreshed marker: %s", refreshedLine)
	}
	// The refreshed revision must be the file's actual post-edit revision.
	current, _, err := service.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if refreshedMetadata.Revision != workspacechange.Revision([]byte(current)) {
		t.Fatalf("refreshed revision %q != current revision %q", refreshedMetadata.Revision, workspacechange.Revision([]byte(current)))
	}
	// The refreshed body must show the CURRENT numbered lines, including the edit.
	for _, want := range []string{"     1\t第一行", "     2\t改写后的第二行", "     3\t新增的一行", "     4\t第三行"} {
		if !strings.Contains(content, want) {
			t.Fatalf("refreshed body missing current line %q: %s", want, content)
		}
	}
	if strings.Contains(content, "     2\t第二行") {
		t.Fatalf("refreshed body must not show the pre-edit line: %s", content)
	}

	// A third assembly over the refreshed body (stored back as history) is
	// stable: the file is unchanged since, so the body stays byte-identical.
	refreshedMessages := []*schema.Message{
		messages[0],
		schema.ToolMessage(content, "call-1", schema.WithToolName("read_file")),
	}
	again := applyToolResultContextPolicyWithResolver(refreshedMessages, idePolicy(256*1024), resolver)
	if len(again) != 2 || again[1].Content != content {
		t.Fatalf("refreshed body must stay byte-identical while the file is unchanged: %#v", again)
	}
}
