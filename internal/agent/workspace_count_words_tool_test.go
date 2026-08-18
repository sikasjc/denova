package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"denova/config"
)

func runCountWordsTool(t *testing.T, workspace string, args string) string {
	t.Helper()
	base, err := newWorkspaceCountWordsTool(workspace)
	if err != nil {
		t.Fatal(err)
	}
	result, err := base.(tool.InvokableTool).InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatalf("count_words run failed: %v", err)
	}
	return result
}

func writeCountWordsWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	files := map[string]string{
		"chapters/ch1.md":          "# 第一章 初雪\n\n雪落了整夜。\n\n她推开门，冷风扑面而来。\n",
		"chapters/ch2.md":          "# 第二章 长街\n\n长街尽头灯火通明。",
		"setting/outline.md":       "# 大纲\n\n卷一：北境。",
		"setting/notes_extra.md":   "first line\nsecond line\nthird line\nfourth line",
		".nova/hidden-config.toml": "ignored = true",
	}
	for rel, content := range files {
		path := filepath.Join(workspace, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return workspace
}

func TestWorkspaceCountWordsToolBookModeMatchesChapterStatistics(t *testing.T) {
	workspace := writeCountWordsWorkspace(t)
	result := runCountWordsTool(t, workspace, `{}`)
	var payload workspaceCountWordsResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Schema != workspaceCountWordsResultSchema || payload.Mode != "book" {
		t.Fatalf("unexpected count_words book header: %#v", payload)
	}
	if payload.ChapterCount != 2 {
		t.Fatalf("chapter count = %d, want 2", payload.ChapterCount)
	}
	// 口径必须与界面统计一致：一个非空白字符记 1 字。
	runeLen := func(s string) int { return len([]rune(s)) }
	wantCh1 := runeLen("#第一章初雪") + runeLen("雪落了整夜。") + runeLen("她推开门，冷风扑面而来。")
	wantCh2 := runeLen("#第二章长街") + runeLen("长街尽头灯火通明。")
	if payload.Chapters[0].Words != wantCh1 || payload.Chapters[1].Words != wantCh2 {
		t.Fatalf("chapter words = %d,%d want %d,%d", payload.Chapters[0].Words, payload.Chapters[1].Words, wantCh1, wantCh2)
	}
	if payload.TotalWords != wantCh1+wantCh2 {
		t.Fatalf("total words = %d, want %d", payload.TotalWords, wantCh1+wantCh2)
	}
	if len(payload.Documents) == 0 || payload.Documents[0].Path != "setting/outline.md" {
		t.Fatalf("outline document missing from count_words result: %#v", payload.Documents)
	}
}

func TestWorkspaceCountWordsToolCountsSpecificFiles(t *testing.T) {
	workspace := writeCountWordsWorkspace(t)
	result := runCountWordsTool(t, workspace, `{"paths":["chapters/ch1.md","`+filepath.Join(workspace, "chapters", "ch2.md")+`","missing.md"]}`)
	var payload workspaceCountWordsResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Mode != "files" || len(payload.Files) != 3 {
		t.Fatalf("unexpected count_words files result: %s", result)
	}
	if payload.Files[0].Words == 0 || payload.Files[1].Words == 0 {
		t.Fatalf("both existing files must be counted: %#v", payload.Files)
	}
	if payload.Files[2].Error == "" || payload.Files[2].Words != 0 {
		t.Fatalf("missing file must carry a per-entry error: %#v", payload.Files[2])
	}
	if payload.TotalWords != payload.Files[0].Words+payload.Files[1].Words {
		t.Fatalf("files total = %d, want sum of entries", payload.TotalWords)
	}
}

func TestWorkspaceCountWordsToolCountsLineRange(t *testing.T) {
	workspace := writeCountWordsWorkspace(t)
	result := runCountWordsTool(t, workspace, `{"paths":["setting/notes_extra.md"],"start_line":2,"end_line":3}`)
	var payload workspaceCountWordsResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatal(err)
	}
	entry := payload.Files[0]
	// 只统计 second line 与 third line 两行。
	if entry.Words != len("secondline")+len("thirdline") {
		t.Fatalf("range words = %d, want %d", entry.Words, len("secondline")+len("thirdline"))
	}
	if entry.StartLine != 2 || entry.EndLine != 3 {
		t.Fatalf("effective range = [%d,%d], want [2,3]", entry.StartLine, entry.EndLine)
	}
	if payload.TotalWords != entry.Words {
		t.Fatalf("range total = %d, want %d", payload.TotalWords, entry.Words)
	}
}

func TestWorkspaceCountWordsToolLineRangeToEndOfFile(t *testing.T) {
	workspace := writeCountWordsWorkspace(t)
	result := runCountWordsTool(t, workspace, `{"paths":["setting/notes_extra.md"],"start_line":3}`)
	var payload workspaceCountWordsResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatal(err)
	}
	entry := payload.Files[0]
	// end_line 省略时统计到末行；文件无尾换行，末行为第 4 行。
	if entry.Words != len("thirdline")+len("fourthline") {
		t.Fatalf("tail range words = %d", entry.Words)
	}
	if entry.EndLine != 4 {
		t.Fatalf("effective end line = %d, want 4", entry.EndLine)
	}
}

func TestWorkspaceCountWordsToolRejectsInvalidLineRange(t *testing.T) {
	workspace := writeCountWordsWorkspace(t)
	base, err := newWorkspaceCountWordsTool(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.(tool.InvokableTool).InvokableRun(context.Background(), `{"paths":["chapters/ch1.md"],"start_line":5,"end_line":2}`); err == nil {
		t.Fatal("end_line before start_line must be rejected")
	}
	if _, err := base.(tool.InvokableTool).InvokableRun(context.Background(), `{"start_line":1,"end_line":2}`); err == nil {
		t.Fatal("line range without paths must be rejected")
	}

	// start_line 超出末行时按条目返回错误，不中断整个调用。
	result := runCountWordsTool(t, workspace, `{"paths":["chapters/ch2.md"],"start_line":99,"end_line":100}`)
	var payload workspaceCountWordsResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Files[0].Error == "" || !strings.Contains(payload.Files[0].Error, "99") {
		t.Fatalf("out-of-range start_line must carry an entry error: %#v", payload.Files[0])
	}
}

func TestWorkspaceCountWordsManifestIsReadOnly(t *testing.T) {
	manifest := ManifestForTool("count_words")
	if manifest.Source != ToolSourceRead {
		t.Fatalf("count_words source = %s, want read", manifest.Source)
	}
	if manifest.Capability != config.AgentToolFileRead {
		t.Fatalf("count_words capability = %s, want %s", manifest.Capability, config.AgentToolFileRead)
	}
	if manifest.MutatesWorkspace {
		t.Fatal("count_words must not mutate the workspace")
	}
	if mode := executionModeForTool(manifest); mode != toolExecutionParallelRead {
		t.Fatalf("count_words execution mode = %v, want parallel read", mode)
	}
}
