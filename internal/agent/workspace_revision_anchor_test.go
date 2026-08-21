package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"denova/internal/workspacechange"
)

func readMetadataRevision(t *testing.T, readTool tool.BaseTool, path string) string {
	t.Helper()
	result, err := readTool.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"`+path+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	metadataLine, _, ok := strings.Cut(result, "\n")
	if !ok {
		t.Fatalf("read result has no metadata line: %q", result)
	}
	var metadata workspaceReadFileMetadata
	if err := json.Unmarshal([]byte(metadataLine), &metadata); err != nil {
		t.Fatal(err)
	}
	return metadata.Revision
}

// The read_file revision anchor must track mutations without a full re-read:
// a successful edit_file seeds the anchor with the committed revision, and an
// external rewrite must invalidate it through stat.
func TestReadFileRevisionAnchorReflectsMutations(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "ideas.md")
	const original = "first\nsecond\n"
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
	if revision := readMetadataRevision(t, readTool, path); revision != workspacechange.Revision([]byte(original)) {
		t.Fatalf("initial anchor = %q", revision)
	}
	if _, err := editTool.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"ideas.md","edits":[{"old_string":"second","new_string":"SECOND LINE"}]}`); err != nil {
		t.Fatal(err)
	}
	const edited = "first\nSECOND LINE\n"
	// A successful edit must seed the anchor for the committed revision instead
	// of forcing the next read to re-hash the whole file.
	if cached, ok := workspaceRevisionAnchors.Load(path); !ok {
		t.Fatal("edit_file did not seed the revision anchor cache")
	} else if anchor := cached.(workspaceRevisionAnchor); anchor.revision != workspacechange.Revision([]byte(edited)) {
		t.Fatalf("seeded anchor = %+v", anchor)
	}
	if revision := readMetadataRevision(t, readTool, path); revision != workspacechange.Revision([]byte(edited)) {
		t.Fatalf("post-edit anchor = %q, want hash of %q", revision, edited)
	}
	// An external rewrite must invalidate the seeded anchor through stat.
	const external = "externally rewritten with a different size\n"
	if err := os.WriteFile(path, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	if revision := readMetadataRevision(t, readTool, path); revision != workspacechange.Revision([]byte(external)) {
		t.Fatalf("post-external anchor = %q", revision)
	}
}

func TestWorkspaceRevisionResolverDetectsExternalChanges(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "notes.md")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := NewWorkspaceRevisionResolver(workspace)
	first, ok := resolver(path)
	if !ok || first != workspacechange.Revision([]byte("v1")) {
		t.Fatalf("resolver returned %q ok=%v", first, ok)
	}
	if err := os.WriteFile(path, []byte("v2 with more bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, ok := resolver(path)
	if !ok || second != workspacechange.Revision([]byte("v2 with more bytes")) || second == first {
		t.Fatalf("resolver did not detect the change: %q vs %q", first, second)
	}
}

func TestEditFileReceiptReportsLineRanges(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "ideas.md")
	const original = "one\ntwo\nthree\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := workspacechange.NewService(workspace)
	if err != nil {
		t.Fatal(err)
	}
	editTool, err := newWorkspaceEditFileTool(service)
	if err != nil {
		t.Fatal(err)
	}
	result, err := editTool.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"ideas.md","file_revision":"`+workspacechange.Revision([]byte(original))+`","edits":[{"start_line":2,"new_string":"TWO\nTWO-B"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	var receipt workspaceChangeToolReceipt
	if err := json.Unmarshal([]byte(result), &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.Edits) != 1 || len(receipt.Edits[0].Hunks) != 1 {
		t.Fatalf("receipt edits = %#v", receipt.Edits)
	}
	hunk := receipt.Edits[0].Hunks[0]
	if hunk.BeforeStartLine != 2 || hunk.BeforeEndLine != 2 || hunk.AfterStartLine != 2 || hunk.AfterEndLine != 3 {
		t.Fatalf("receipt hunk line range = %+v", hunk)
	}
	if receipt.Edits[0].ID == "" || receipt.Edits[0].Replacements != 1 {
		t.Fatalf("receipt edit identity = %#v", receipt.Edits[0])
	}
}
