package prompts

import (
	"strings"
	"testing"
)

// TestWritingFlowInjectsDefaultStructureWhenUnset confirms the built-in outline
// and chapter-group structures appear in the writing-flow instruction when the
// user has not configured a custom template.
func TestWritingFlowInjectsDefaultStructureWhenUnset(t *testing.T) {
	flow := BuildIDEWritingFlowInstruction(SystemInstructionInput{Workspace: "/ws"})
	if !strings.Contains(flow, "大纲与细纲结构") {
		t.Fatalf("writing flow must inject the structure section: %s", flow)
	}
	if !strings.Contains(flow, defaultOutlineFormat) {
		t.Fatal("writing flow must inject the default outline structure when unset")
	}
	if !strings.Contains(flow, defaultChapterGroupFormat) {
		t.Fatal("writing flow must inject the default chapter-group structure when unset")
	}
}

// TestWritingFlowInjectsCustomStructure confirms a user-configured template
// replaces the built-in default in the injected writing-flow instruction.
func TestWritingFlowInjectsCustomStructure(t *testing.T) {
	custom := "# 自定义大纲\n\n## 主线\n- 目标：{goal}"
	flow := BuildIDEWritingFlowInstruction(SystemInstructionInput{
		Workspace:     "/ws",
		OutlineFormat: custom,
	})
	if !strings.Contains(flow, custom) {
		t.Fatalf("writing flow must inject the user outline structure: %s", flow)
	}
	if strings.Contains(flow, defaultOutlineFormat) {
		t.Fatal("user outline structure must replace the built-in default, not append to it")
	}
	// Chapter-group format is still unset, so its default must remain.
	if !strings.Contains(flow, defaultChapterGroupFormat) {
		t.Fatal("unset chapter-group structure should still use the default")
	}
}
