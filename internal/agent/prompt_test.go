package agent

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/config"
	"denova/internal/book"
	"denova/internal/prompts"
)

func TestBuildInteractiveStoryInstructionDoesNotLogDuringPromptBuild(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(previous)
	})

	state := book.NewState(t.TempDir())
	composition := BuildInteractiveStoryInstructionComposition(&config.Config{Workspace: state.Workspace()}, state, prompts.InteractiveStorySystemInstructionInput{
		StoryTellerID:           "classic",
		StoryTellerSystemPrompt: "讲述规则",
	})
	if composition.Instruction() == "" {
		t.Fatal("composition instruction should be populated")
	}
	if got := buf.String(); strings.Contains(got, "[agent-prompt]") {
		t.Fatalf("prompt build should not emit agent-prompt logs, got:\n%s", got)
	}

	composition.logForRun(RunOptions{TaskID: "task-1", SessionID: "session-1"})
	got := buf.String()
	if count := strings.Count(got, "[agent-prompt] system composition"); count != 1 {
		t.Fatalf("expected one composition log, got %d:\n%s", count, got)
	}
	if !strings.Contains(got, "task_id=task-1") || !strings.Contains(got, "session_id=session-1") {
		t.Fatalf("composition log should include run identifiers:\n%s", got)
	}
}

func TestBuildInstructionKeepsWorkspaceStateOutOfSystemPrompt(t *testing.T) {
	state := book.NewState(t.TempDir())
	if err := state.InitWorkspace(); err != nil {
		t.Fatalf("InitWorkspace failed: %v", err)
	}
	if err := os.MkdirAll(state.SettingDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state.SettingDir(), "outline.md"), []byte("主角进入废城。"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Workspace: state.Workspace()}

	instruction := BuildInstruction(cfg, state, IDEStoryTeller{})
	if strings.Contains(instruction, "主角进入废城") || strings.Contains(instruction, "# 当前作品状态") {
		t.Fatalf("system prompt should not include dynamic workspace state:\n%s", instruction)
	}
	contexts := IDEWorkspaceRuntimeContextsForState(state)
	if !strings.Contains(contexts.Stable, "主角进入废城") {
		t.Fatalf("stable runtime workspace context should include outline: %#v", contexts)
	}
	if strings.Contains(contexts.Dynamic, "主角进入废城") {
		t.Fatalf("dynamic runtime workspace context should not include stable outline: %#v", contexts)
	}
	if context := IDEWorkspaceRuntimeContext(state); !strings.Contains(context, "主角进入废城") {
		t.Fatalf("legacy runtime workspace context should include state: %q", context)
	}
}

func TestBuildInstructionUsesPerBookStructureFormatFiles(t *testing.T) {
	state := book.NewState(t.TempDir())
	if err := state.InitWorkspace(); err != nil {
		t.Fatalf("InitWorkspace failed: %v", err)
	}
	const customOutline = "# 本书专属大纲结构\n\n## 三幕分卷"
	const customChapterGroup = "# 本书专属细纲结构\n\n## 场景节拍"
	if err := os.WriteFile(filepath.Join(state.SettingDir(), book.OutlineFormatFileName), []byte(customOutline), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state.SettingDir(), book.ChapterGroupFormatFileName), []byte(customChapterGroup), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Workspace: state.Workspace()}

	instruction := BuildInstruction(cfg, state, IDEStoryTeller{})
	for _, want := range []string{customOutline, customChapterGroup} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("runtime system prompt should include per-book structure %q:\n%s", want, instruction)
		}
	}
	if strings.Contains(instruction, "## 分卷规划") {
		t.Fatal("per-book outline structure must replace the built-in default in the runtime prompt")
	}

	blocks := BuiltinAgentPromptBlocks(cfg, state, IDEStoryTeller{})
	for _, want := range []string{customOutline, customChapterGroup} {
		if !strings.Contains(blocks.IDE.EditableSystemPrompt, want) {
			t.Fatalf("Agents-page flow preview should match runtime structure %q:\n%s", want, blocks.IDE.EditableSystemPrompt)
		}
	}
	sources := BuiltinAgentPromptSources(cfg, state, IDEStoryTeller{})
	flowSource := findPromptSource(sources.IDE.Sources, "flow")
	if flowSource == nil || !strings.Contains(flowSource.Source, book.OutlineFormatFileName) || !strings.Contains(flowSource.Source, book.ChapterGroupFormatFileName) {
		t.Fatalf("Agents-page flow source should name both per-book files: %#v", flowSource)
	}
}

func TestBuildInstructionFallsBackToBuiltInStructuresWithoutFiles(t *testing.T) {
	state := book.NewState(t.TempDir()) // Deliberately do not InitWorkspace.
	instruction := BuildInstruction(&config.Config{Workspace: state.Workspace()}, state, IDEStoryTeller{})
	for _, want := range []string{"## 一句话简介", "## 分卷规划", "## 章节组目标", "## 逐章安排"} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("missing per-book file should fall back to built-in structure marker %q", want)
		}
	}
}

func TestIDEContextRuntimeContextIsBoundedPathOnlyState(t *testing.T) {
	longName := strings.Repeat("长", ideContextMaxPathRunes+8) + ".md"
	context := IDEContextRuntimeContext(IDEContextRef{
		CurrentFile: "/chapters/ch01.md",
		OpenFiles: []string{
			"chapters/ch01.md",
			"chapters/ch01.md",
			"../outside.md",
			longName,
		},
	})

	for _, want := range []string{"当前聚焦文件：chapters/ch01.md", "当前打开文件：chapters/ch01.md、", "不包含文件正文", "必须按路径显式使用工具读取", "[已截断]"} {
		if !strings.Contains(context, want) {
			t.Fatalf("IDE context missing %q:\n%s", want, context)
		}
	}
	if strings.Contains(context, "../outside.md") {
		t.Fatalf("IDE context should drop unsafe relative paths:\n%s", context)
	}
	if strings.Count(context, "chapters/ch01.md") != 2 {
		t.Fatalf("IDE context should dedupe open files while preserving current file:\n%s", context)
	}
}

func findPromptSource(sources []config.AgentPromptSource, id string) *config.AgentPromptSource {
	for i := range sources {
		if sources[i].ID == id {
			return &sources[i]
		}
	}
	return nil
}
