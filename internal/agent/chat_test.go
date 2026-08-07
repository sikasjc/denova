package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"denova/internal/book"
)

func TestMergeToolCalls(t *testing.T) {
	idx := 0
	calls := mergeToolCalls(nil, []schema.ToolCall{
		{Index: &idx, Function: schema.FunctionCall{Name: "write_file", Arguments: `{"path":`}},
	})
	calls = mergeToolCalls(calls, []schema.ToolCall{
		{Index: &idx, Function: schema.FunctionCall{Arguments: `"chapters/ch01.md"}`}},
	})

	if len(calls) != 1 {
		t.Fatalf("期望 1 个 tool call，实际: %d", len(calls))
	}
	if calls[0].Function.Name != "write_file" {
		t.Fatalf("工具名称未合并: %s", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"path":"chapters/ch01.md"}` {
		t.Fatalf("工具参数未合并: %s", calls[0].Function.Arguments)
	}
}

func TestMergeToolCallsHandlesSparseIndexes(t *testing.T) {
	idx := 2
	calls := mergeToolCalls(nil, []schema.ToolCall{
		{Index: &idx, ID: "call-2", Function: schema.FunctionCall{Name: "edit_file", Arguments: `{"path":`}},
	})
	calls = mergeToolCalls(calls, []schema.ToolCall{
		{Index: &idx, Function: schema.FunctionCall{Arguments: `"chapters/ch02.md"}`}},
	})

	if len(calls) != 3 {
		t.Fatalf("稀疏 index 应补齐切片长度，实际: %d", len(calls))
	}
	if calls[2].ID != "call-2" || calls[2].Function.Name != "edit_file" {
		t.Fatalf("工具元信息未按 index 保留: %#v", calls[2])
	}
	if calls[2].Function.Arguments != `{"path":"chapters/ch02.md"}` {
		t.Fatalf("工具参数未按 index 合并: %s", calls[2].Function.Arguments)
	}
}

func TestParseWriteLoreItemsToolResultReturnsChangedIDs(t *testing.T) {
	itemIDs, deletedIDs := parseWriteLoreItemsToolResult("write_lore_items", strings.Join([]string{
		"message: 已更新资料库",
		`item_ids: ["char_hero","world_rule"]`,
		`deleted_ids: ["old_note"]`,
	}, "\n"))

	if got := strings.Join(itemIDs, ","); got != "char_hero,world_rule" {
		t.Fatalf("未解析写入资料 ID: %v", itemIDs)
	}
	if got := strings.Join(deletedIDs, ","); got != "old_note" {
		t.Fatalf("未解析删除资料 ID: %v", deletedIDs)
	}
}

func TestComposeAgentInputDoesNotInjectImagePresetContext(t *testing.T) {
	composition := composeAgentInput(ChatRequest{
		Message:       "给当前章节生成插画",
		ImagePresetID: "realistic",
		ImagePreset: ImagePresetContext{
			ID:                "realistic",
			Name:              "写实",
			AgentSystemPrompt: "系统理解规则。",
			ToolRequestPrompt: "真实光影和摄影感。",
		},
	}, nil, nil, DefaultLoopPolicy())
	if strings.Contains(composition.AgentMessage, "真实光影和摄影感") || strings.Contains(composition.AgentMessage, "图像方案预设") {
		t.Fatalf("image preset should not be injected into turn message:\n%s", composition.AgentMessage)
	}
	if composition.ContextLog != nil && strings.Contains(composition.ContextLog.String(), "图像方案预设") {
		t.Fatalf("context log should not record image preset as turn context:\n%s", composition.ContextLog.String())
	}
}

func TestComposeAgentInputPlacesAttachmentsBeforeRawRequest(t *testing.T) {
	workspace := t.TempDir()
	mustWriteTestFile(t, workspace, "chapters/reference.md", "引用正文")
	composition := composeAgentInput(ChatRequest{
		Message:      "只润色这一句",
		WritingSkill: "novel-lite",
		LoadedWritingSkill: &LoadedWritingSkill{
			Name:          "novel-lite",
			Content:       "# novel-lite\n\n执行最小修改",
			BaseDirectory: "/app/skills/novel-lite",
		},
		References: []string{"chapters/reference.md"},
		Selections: []TextSelectionRef{{
			FileName:  "chapters/current.md",
			StartLine: 3,
			EndLine:   3,
			Content:   "待修改原句",
		}},
	}, nil, book.NewService(workspace), DefaultLoopPolicy())

	message := composition.AgentMessage
	for _, attachment := range []string{
		"# 已加载的内置 Writing Skill",
		"# 用户引用的文件",
		"# 编辑器选区",
		"[上下文边界]",
	} {
		index := strings.Index(message, attachment)
		requestIndex := strings.LastIndex(message, "# 本轮用户请求（最高优先级）")
		if index < 0 || requestIndex < 0 || index >= requestIndex {
			t.Fatalf("attachment %q must precede the final request:\n%s", attachment, message)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(message), "只润色这一句") {
		t.Fatalf("raw request must be the absolute tail of the turn message:\n%s", message)
	}
	if strings.Count(message, "只润色这一句") != 1 {
		t.Fatalf("raw request should appear exactly once, got %d:\n%s", strings.Count(message, "只润色这一句"), message)
	}
}

func TestAppendReferenceContextDedupesAndReportsReadFailure(t *testing.T) {
	workspace := t.TempDir()
	mustWriteTestFile(t, workspace, "chapters/ch01.md", "第一章正文")
	service := book.NewService(workspace)

	got := appendReferenceContext(service, "请参考", []string{
		"chapters/ch01.md",
		"chapters/ch01.md",
		"chapters/missing.md",
	})

	assertContains(t, got, "请参考")
	assertContains(t, got, "# 用户引用的文件")
	assertContains(t, got, "以下文件由用户在本轮显式引用")
	assertContains(t, got, "## @chapters/ch01.md")
	assertContains(t, got, "```markdown\n第一章正文\n```")
	assertContains(t, got, "## @chapters/missing.md")
	assertContains(t, got, "读取失败：")
	if count := strings.Count(got, "## @chapters/ch01.md"); count != 1 {
		t.Fatalf("重复引用应去重，实际出现 %d 次\n%s", count, got)
	}
}

func TestAppendSelectionContextIncludesFileAndLineRange(t *testing.T) {
	got := appendSelectionContext("修改这段", []TextSelectionRef{
		{
			FileName:  "chapters/ch03.md",
			StartLine: 12,
			EndLine:   18,
			Content:   "选中的正文",
		},
	})

	assertContains(t, got, "修改这段")
	assertContains(t, got, "# 编辑器选区")
	assertContains(t, got, "以下文本由用户在本轮显式选中")
	assertContains(t, got, "## 选中内容来自 chapters/ch03.md:L12-L18")
	assertContains(t, got, "```\n选中的正文\n```")
}

func TestBoundedStyleRulesBoundsReferenceIndex(t *testing.T) {
	got := boundedStyleRules([]StyleRule{{
		Scene: "日常对话",
		StyleReferences: []StyleReference{
			{Name: "短", Path: "/tmp/.denova/styles/short.md", DisplayPath: ".denova/styles/short.md"},
			{Name: strings.Repeat("长", 100), Description: strings.Repeat("风", 100), Path: "/tmp/.denova/styles/long.md", DisplayPath: ".denova/styles/long.md"},
		},
	}}, 120)
	if len(got) != 1 || len(got[0].StyleReferences) != 1 {
		t.Fatalf("bounded refs = %#v, want only first ref", got)
	}
	if got[0].StyleReferences[0].Name != "短" {
		t.Fatalf("first ref mismatch: %#v", got[0].StyleReferences[0])
	}
}

func TestShouldResumeInterruptedRequestOnlyMatchesExplicitContinue(t *testing.T) {
	if !shouldResumeInterruptedRequest("继续") {
		t.Fatal("明确的继续请求应触发异常恢复")
	}
	if !shouldResumeInterruptedRequest("继续刚才的任务") {
		t.Fatal("继续刚才的任务应触发异常恢复")
	}
	if shouldResumeInterruptedRequest("帮我写下一章") {
		t.Fatal("普通请求不应触发异常恢复")
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("期望包含 %q\n实际内容:\n%s", want, got)
	}
}

func mustWriteTestFile(t *testing.T, workspace, relPath, content string) {
	t.Helper()
	absPath := filepath.Join(workspace, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("创建测试目录失败: %v", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
}
