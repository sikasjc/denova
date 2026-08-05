package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinWritingPresetInstructionsCoverScopeInference(t *testing.T) {
	for _, name := range []string{"novel-lite", "novel-standard", "novel-heavy"} {
		content := readBuiltinWritingPreset(t, name)
		for _, required := range []string{
			"agent: ide",
			"不要假设任务一定是下一章",
			"没有 `writing_scope` 字段",
		} {
			if !strings.Contains(content, required) {
				t.Fatalf("%s missing required instruction %q", name, required)
			}
		}
	}
}

func TestBuiltinWritingPresetInstructionsCoverMultiChapterPlanning(t *testing.T) {
	for _, name := range []string{"novel-standard", "novel-heavy"} {
		content := readBuiltinWritingPreset(t, name)
		for _, required := range []string{
			"整体计划",
			"分章计划",
		} {
			if !strings.Contains(content, required) {
				t.Fatalf("%s missing multi-chapter planning instruction %q", name, required)
			}
		}
	}
}

func TestBuiltinWritingPresetInstructionsKeepWorkflowSpecificTools(t *testing.T) {
	for name, required := range map[string][]string{
		"novel-lite":     {"read_file", "write_file", "edit_file", "[tool error]", "不得宣称已完成"},
		"novel-standard": {"write_file", "edit_file", "[tool error]", "不得宣称已完成", "reviewer"},
		"novel-heavy":    {"edit_file", "[tool error]", "不得宣称已完成", "context-planner", "fixer", "final-gate", "memory-patcher"},
	} {
		content := readBuiltinWritingPreset(t, name)
		for _, value := range required {
			if !strings.Contains(content, value) {
				t.Fatalf("%s missing workflow instruction %q", name, value)
			}
		}
	}
}

func TestBuiltinWritingPresetInstructionsUseMinimalPatchContract(t *testing.T) {
	for _, name := range []string{"novel-lite", "novel-standard", "novel-heavy"} {
		content := readBuiltinWritingPreset(t, name)
		for _, required := range []string{
			"最小必要",
			"edit_file",
		} {
			if !strings.Contains(content, required) {
				t.Fatalf("%s missing minimal patch instruction %q", name, required)
			}
		}
	}
	rewrite := readBuiltinWritingPreset(t, "rewrite")
	for _, required := range []string{"最小必要", "edit_file", "old_string", "整章重写"} {
		if !strings.Contains(rewrite, required) {
			t.Fatalf("rewrite missing minimal patch instruction %q", required)
		}
	}
	for _, forbidden := range []string{
		"根据作者要求进行修改，完全重写",
		"使用 write_file 写回修改后的章节",
	} {
		if strings.Contains(rewrite, forbidden) {
			t.Fatalf("rewrite skill still defaults to full replacement: %q", forbidden)
		}
	}
}

func TestBuiltinWritingPresetInstructionsStayCompactForInlineDelivery(t *testing.T) {
	maxBytes := map[string]int{
		"novel-lite":     2500,
		"novel-standard": 4500,
		"novel-heavy":    8000,
	}
	for name, maximum := range maxBytes {
		content := readBuiltinWritingPreset(t, name)
		if len(content) > maximum {
			t.Fatalf("%s grew to %d bytes, want <= %d for inline delivery", name, len(content), maximum)
		}
	}
}

func TestBuiltinWritingPresetInstructionsCoverTaskDelegation(t *testing.T) {
	for _, name := range []string{"novel-standard", "novel-heavy"} {
		content := readBuiltinWritingPreset(t, name)
		for _, required := range []string{
			"task",
			"description",
			"reviewer",
		} {
			if !strings.Contains(content, required) {
				t.Fatalf("%s missing task delegation instruction %q", name, required)
			}
		}
	}
}

func TestBuiltinWritingPresetChoreographyPolicy(t *testing.T) {
	lite := readBuiltinWritingPreset(t, "novel-lite")
	if !strings.Contains(lite, "禁止启动 reviewer、fixer、task 或 General SubAgent") {
		t.Fatalf("novel-lite should keep choreography and all SubAgents disabled")
	}
	for _, forbidden := range []string{"choreographer", "intimacy-choreographer"} {
		if strings.Contains(lite, forbidden) {
			t.Fatalf("novel-lite should not reference %q", forbidden)
		}
	}

	standard := readBuiltinWritingPreset(t, "novel-standard")
	for _, required := range []string{
		"按需编排",
		"subagent_type=choreographer",
		"subagent_type=intimacy-choreographer",
		"同一场景最多调用一个 choreography SubAgent 一次",
		"普通对白",
		"不自行升级或净化",
	} {
		if !strings.Contains(standard, required) {
			t.Fatalf("novel-standard missing choreography policy %q", required)
		}
	}

	heavy := readBuiltinWritingPreset(t, "novel-heavy")
	for _, required := range []string{
		"context-planner -> choreographer/intimacy-choreographer -> writer",
		"Context Plan 标记复杂编排风险",
		"## Choreography Need",
		"不需要 choreography",
		"不自行升级或净化",
	} {
		if !strings.Contains(heavy, required) {
			t.Fatalf("novel-heavy missing choreography policy %q", required)
		}
	}
}

func TestBuiltinChapterIllustrationSkillIsIDEOnly(t *testing.T) {
	content := readBuiltinWritingPreset(t, "chapter-illustration")
	for _, required := range []string{
		"name: chapter-illustration",
		"agent: ide",
		"generate_image",
		"不要自动编辑章节正文",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("chapter-illustration missing required instruction %q", required)
		}
	}
}

func TestBuiltinLoreSkillCoversToolUsage(t *testing.T) {
	content := readBuiltinWritingPreset(t, "lore")
	for _, required := range []string{
		"name: lore",
		"agent: ide,config_manager,interactive_story",
		"list_lore_items",
		"read_lore_items",
		"write_lore_items",
		"`list_lore_items` 全量索引",
		"`read_lore_items` 批量读取正文",
		"`write_lore_items` 创建或更新条目",
		`"delete_ids": []`,
		`"items":[],"delete_ids":["old-hero-draft"]`,
		"`delete_ids` 必须是数组",
		"不要传字符串 `\"[]\"`",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("lore skill missing required instruction %q", required)
		}
	}
}

func readBuiltinWritingPreset(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "skills", name, SkillFileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
