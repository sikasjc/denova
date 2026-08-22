package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"denova/internal/workspacechange"
)

func TestWorkspaceReadFileDefaultsToCompactWindow(t *testing.T) {
	if agentFileReadDefaultLimitLines != 300 {
		t.Fatalf("default read window = %d, want 300", agentFileReadDefaultLimitLines)
	}
	if len(workspaceReadFileToolDescription) > 700 {
		t.Fatalf("read_file description is too large: %d bytes", len(workspaceReadFileToolDescription))
	}
}

func TestWorkspaceReadFileToolReturnsPartialWindowWithFullFileRevision(t *testing.T) {
	content := "first\nsecond\nthird\nfourth"
	path := writeTempFile(t, content)
	base, err := newWorkspaceReadFileTool(newTestAgentFilesystemBackend(t))
	if err != nil {
		t.Fatal(err)
	}
	result, err := base.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"`+path+`","offset":2,"limit":1}`)
	if err != nil {
		t.Fatal(err)
	}
	metadataLine, body, ok := strings.Cut(result, "\n")
	if !ok {
		t.Fatalf("read result has no metadata line: %q", result)
	}
	var metadata workspaceReadFileMetadata
	if err := json.Unmarshal([]byte(metadataLine), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Schema != workspaceReadFileResultSchema || metadata.Offset != 2 || metadata.Limit != 1 {
		t.Fatalf("unexpected read metadata: %#v", metadata)
	}
	// The anchor hashes the FULL file (not the returned window) so a later turn
	// can detect any change to the file and force a targeted re-read.
	if metadata.Revision != workspacechange.Revision([]byte(content)) {
		t.Fatalf("read metadata revision must anchor the full file: got %q want %q", metadata.Revision, workspacechange.Revision([]byte(content)))
	}
	if !strings.Contains(body, "     2\tsecond") || strings.Contains(body, "first") || strings.Contains(body, "third") {
		t.Fatalf("partial cat-n selection mismatch: %q", body)
	}
}

func TestWorkspaceReadFileToolReturnsOnlyRealStableLineNumbers(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "LF", content: "first\nsecond\n", want: "     1\tfirst\n     2\tsecond"},
		{name: "CRLF", content: "first\r\nsecond\r\n", want: "     1\tfirst\n     2\tsecond"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeTempFile(t, test.content)
			base, err := newWorkspaceReadFileTool(newTestAgentFilesystemBackend(t))
			if err != nil {
				t.Fatal(err)
			}
			result, err := base.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"`+path+`"}`)
			if err != nil {
				t.Fatal(err)
			}
			_, body, ok := strings.Cut(result, "\n")
			if !ok {
				t.Fatalf("read result has no metadata line: %q", result)
			}
			if body != test.want {
				t.Fatalf("line-numbered body = %q", body)
			}
			if strings.Contains(body, "     3\t") {
				t.Fatalf("trailing newline created a non-existent source line: %q", body)
			}
		})
	}
}

func TestWorkspaceReadFileToolOmitsRevisionForOversizedFile(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "huge.txt")
	// Below the per-window selection cap but above the full-file revision cap, so
	// the tool returns a window yet cannot compute a bounded anchor.
	line := strings.Repeat("a", 4096) + "\n"
	var builder strings.Builder
	for builder.Len() <= workspaceReadFileRevisionMaxBytes {
		builder.WriteString(line)
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	base, err := newWorkspaceReadFileTool(newTestAgentFilesystemBackend(t, workspace), workspace)
	if err != nil {
		t.Fatal(err)
	}
	result, err := base.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"`+path+`","offset":1,"limit":1}`)
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
	if metadata.Revision != "" {
		t.Fatalf("oversized file must omit the revision anchor, got %q", metadata.Revision)
	}
}

func TestWorkspaceReadFileToolPreservesDefaultWindowSchema(t *testing.T) {
	base, err := newWorkspaceReadFileTool(newTestAgentFilesystemBackend(t))
	if err != nil {
		t.Fatal(err)
	}
	info, err := base.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	for _, property := range []string{`"file_path"`, `"offset"`, `"limit"`, `"revision_only"`} {
		if !strings.Contains(string(raw), property) {
			t.Fatalf("read_file schema is missing %s: %s", property, raw)
		}
	}
	for _, expected := range []string{
		"revision_only=true",
		"without file content",
		"after a revision conflict",
	} {
		if !strings.Contains(workspaceReadFileToolDescription, expected) {
			t.Fatalf("read_file description missing %q:\n%s", expected, workspaceReadFileToolDescription)
		}
	}
}

func TestWorkspaceReplaceLinesUsesRevisionGuard(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "ideas.md")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("manual update"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := workspacechange.NewService(workspace)
	if err != nil {
		t.Fatal(err)
	}
	replaceLinesTool, err := newWorkspaceReplaceLinesTool(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = replaceLinesTool.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"ideas.md","file_revision":"`+workspacechange.Revision([]byte("manual update"))+`","replacements":[{"start_line":1,"content":"agent update"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "agent update" {
		t.Fatalf("replace_lines did not apply against its revision-checked snapshot: %q", content)
	}
}

func TestWorkspaceReadThenEditByLineUsesRevisionGuard(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "ideas.md")
	const original = "first\nsecond\nthird\n"
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
	readResult, err := readTool.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"`+path+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	metadataLine, body, ok := strings.Cut(readResult, "\n")
	if !ok {
		t.Fatalf("read result has no metadata: %q", readResult)
	}
	var metadata workspaceReadFileMetadata
	if err := json.Unmarshal([]byte(metadataLine), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Revision == "" || !strings.Contains(body, "     2\tsecond") {
		t.Fatalf("read result cannot safely drive a line edit: metadata=%#v body=%q", metadata, body)
	}
	editTool, err := newWorkspaceReplaceLinesTool(service)
	if err != nil {
		t.Fatal(err)
	}
	lineEdit := fmt.Sprintf(`{
		"file_path":"ideas.md",
		"file_revision":%q,
		"replacements":[{"start_line":2,"content":"SECOND"}]
	}`, metadata.Revision)
	if _, err := editTool.(tool.InvokableTool).InvokableRun(context.Background(), lineEdit); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "first\nSECOND\nthird\n" {
		t.Fatalf("line edit result = %q err=%v", content, err)
	}

	staleRevision := workspacechange.Revision([]byte("first\nSECOND\nthird\n"))
	const external = "inserted\nfirst\nSECOND\nthird\n"
	if err := os.WriteFile(path, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	staleEdit := fmt.Sprintf(`{
		"file_path":"ideas.md",
		"file_revision":%q,
		"replacements":[{"start_line":2,"content":"WRONG TARGET"}]
	}`, staleRevision)
	if _, err := editTool.(tool.InvokableTool).InvokableRun(context.Background(), staleEdit); err == nil {
		t.Fatal("stale line numbers should be rejected")
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != external {
		t.Fatalf("stale line edit changed workspace: %q err=%v", content, err)
	}
}

func TestWorkspaceReadFileToolRejectsPathOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := writeTempFile(t, "outside")
	base, err := newWorkspaceReadFileTool(newTestAgentFilesystemBackend(t, workspace), workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, err = base.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"`+outside+`"}`)
	if err == nil || !strings.Contains(err.Error(), "outside the active workspace") {
		t.Fatalf("outside read should be rejected, got %v", err)
	}
}

func TestWorkspaceReadFileToolBoundsOneVeryLongLine(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "long.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", workspaceReadFileMaxSelectedBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	base, err := newWorkspaceReadFileTool(newTestAgentFilesystemBackend(t, workspace), workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, err = base.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"`+path+`"}`)
	if err == nil || !strings.Contains(err.Error(), "selected read_file window exceeds") {
		t.Fatalf("oversized selected line should be rejected, got %v", err)
	}
}

func TestWorkspaceReadFileToolRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	workspace := t.TempDir()
	outside := writeTempFile(t, "outside")
	link := filepath.Join(workspace, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	base, err := newWorkspaceReadFileTool(newTestAgentFilesystemBackend(t, workspace), workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, err = base.(tool.InvokableTool).InvokableRun(context.Background(), `{"file_path":"`+link+`"}`)
	if err == nil {
		t.Fatal("workspace read must not follow a symlink outside the active workspace")
	}
}
