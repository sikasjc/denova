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

func TestWorkspaceReadFileRevisionOnlyReturnsCompactMetadata(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "ideas.md")
	content := strings.Repeat("large line content\n", 200)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	base, err := newWorkspaceReadFileTool(newTestAgentFilesystemBackend(t, workspace), workspace)
	if err != nil {
		t.Fatal(err)
	}
	result, err := base.(tool.InvokableTool).InvokableRun(context.Background(), fmt.Sprintf(`{
		"file_path":%q,
		"revision_only":true,
		"offset":999,
		"limit":999
	}`, path))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, "\n") || strings.Contains(result, "large line content") {
		t.Fatalf("revision_only must not return file content: %q", result)
	}
	var metadata workspaceReadFileMetadata
	if err := json.Unmarshal([]byte(result), &metadata); err != nil {
		t.Fatal(err)
	}
	if !metadata.RevisionOnly || metadata.FilePath != path || metadata.Size != int64(len(content)) {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if metadata.Revision != workspacechange.Revision([]byte(content)) {
		t.Fatalf("revision = %q, want %q", metadata.Revision, workspacechange.Revision([]byte(content)))
	}
	if metadata.Offset != 0 || metadata.Limit != 0 {
		t.Fatalf("revision_only should ignore offset/limit: %#v", metadata)
	}
}

func TestWorkspaceReadFileRevisionOnlyRefreshesAfterWrite(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "ideas.md")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	base, err := newWorkspaceReadFileTool(newTestAgentFilesystemBackend(t, workspace), workspace)
	if err != nil {
		t.Fatal(err)
	}
	invokable := base.(tool.InvokableTool)
	readRevision := func() string {
		result, runErr := invokable.InvokableRun(context.Background(), fmt.Sprintf(`{"file_path":%q,"revision_only":true}`, path))
		if runErr != nil {
			t.Fatal(runErr)
		}
		var metadata workspaceReadFileMetadata
		if unmarshalErr := json.Unmarshal([]byte(result), &metadata); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		return metadata.Revision
	}
	before := readRevision()
	if err := os.WriteFile(path, []byte("after content"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := readRevision()
	if before == after || after != workspacechange.Revision([]byte("after content")) {
		t.Fatalf("revision_only did not observe write: before=%q after=%q", before, after)
	}
}
