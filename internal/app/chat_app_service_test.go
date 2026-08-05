package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/config"
	"denova/internal/agent"
)

func TestApplyWritingSkillRuntimePolicyResolvesDefaultNameOnly(t *testing.T) {
	runtime := &ideChatRuntime{cfg: config.Config{
		WritingSkillDefault: "novel-heavy",
		SubAgents: []config.SubAgentConfig{{
			ID:           "researcher",
			Description:  "Reads context.",
			SystemPrompt: "Return notes.",
		}},
	}}
	req := &agent.ChatRequest{Message: "帮我分析一下 progress.md 有没有问题"}

	if err := applyWritingSkillRuntimePolicy(context.Background(), runtime, req); err != nil {
		t.Fatal(err)
	}
	if req.WritingSkill != "novel-heavy" {
		t.Fatalf("writing skill = %s, want novel-heavy", req.WritingSkill)
	}
	if len(runtime.cfg.SubAgents) != 1 || runtime.cfg.SubAgents[0].ID != "researcher" {
		t.Fatalf("writing skill selection should not mutate subagents: %+v", runtime.cfg.SubAgents)
	}
	if req.LoadedWritingSkill != nil {
		t.Fatalf("non-writing analysis should not inline a Writing Skill: %#v", req.LoadedWritingSkill)
	}
}

func TestApplyWritingSkillRuntimePolicyKeepsCustomSkillAsDynamicHintOnly(t *testing.T) {
	runtime := &ideChatRuntime{cfg: config.Config{WritingSkillDefault: "novel-standard"}}
	req := &agent.ChatRequest{Message: "写一个雨夜重逢的场景", WritingSkill: "slow-burn"}

	if err := applyWritingSkillRuntimePolicy(context.Background(), runtime, req); err != nil {
		t.Fatal(err)
	}
	if req.WritingSkill != "slow-burn" {
		t.Fatalf("writing skill = %s, want slow-burn", req.WritingSkill)
	}
	if runtime.cfg.GeneralSubAgents.IDE != nil || len(runtime.cfg.SubAgents) != 0 {
		t.Fatalf("writing skill selection should not mutate agent config: %+v", runtime.cfg)
	}
}

func TestApplyWritingSkillRuntimePolicyInlinesActiveBuiltinPreset(t *testing.T) {
	root := t.TempDir()
	builtin := filepath.Join(root, "builtin")
	writeTestSkill(t, builtin, "novel-lite", "fast preset")
	writeTestSkill(t, builtin, "continue", "continue workflow")
	writeTestSkill(t, builtin, "rewrite", "rewrite workflow")
	runtime := &ideChatRuntime{workspace: filepath.Join(root, "workspace"), cfg: config.Config{
		SkillsDir:           builtin,
		WritingSkillDefault: "novel-lite",
	}}
	req := &agent.ChatRequest{Message: "续写下一章"}

	if err := applyWritingSkillRuntimePolicy(context.Background(), runtime, req); err != nil {
		t.Fatal(err)
	}
	if req.LoadedWritingSkill == nil || req.LoadedWritingSkill.Name != "novel-lite" || req.LoadedWritingSkill.Content != "fast preset" {
		t.Fatalf("loaded writing skill = %#v", req.LoadedWritingSkill)
	}
	for _, name := range []string{"novel-lite", "novel-standard", "novel-heavy", "continue", "rewrite"} {
		if enabled, ok := runtime.cfg.AgentSkills.IDE[name]; !ok || enabled {
			t.Fatalf("expected %s hidden from request skill tool: %#v", name, runtime.cfg.AgentSkills.IDE)
		}
	}
}

func TestApplyWritingSkillRuntimePolicyUsesStructuredIntentForRealQuickActionPrompt(t *testing.T) {
	root := t.TempDir()
	builtin := filepath.Join(root, "builtin")
	writeTestSkill(t, builtin, "novel-lite", "fast preset")
	runtime := &ideChatRuntime{workspace: filepath.Join(root, "workspace"), cfg: config.Config{
		SkillsDir:           builtin,
		WritingSkillDefault: "novel-lite",
	}}
	req := &agent.ChatRequest{
		Message:       "请读取当前章节组细纲和最近正文，完成目标章节。",
		WritingIntent: config.WritingIntentProseGeneration,
	}

	if err := applyWritingSkillRuntimePolicy(context.Background(), runtime, req); err != nil {
		t.Fatal(err)
	}
	if req.LoadedWritingSkill == nil || req.LoadedWritingSkill.Content != "fast preset" {
		t.Fatalf("structured quick action intent should inline preset: %#v", req.LoadedWritingSkill)
	}
}

func TestApplyWritingSkillRuntimePolicyFallsBackWhenBuiltinPresetExceedsInlineLimit(t *testing.T) {
	root := t.TempDir()
	builtin := filepath.Join(root, "builtin")
	writeTestSkill(t, builtin, "novel-lite", strings.Repeat("x", 128*1024+1))
	runtime := &ideChatRuntime{workspace: filepath.Join(root, "workspace"), cfg: config.Config{
		SkillsDir:           builtin,
		WritingSkillDefault: "novel-lite",
	}}
	req := &agent.ChatRequest{Message: "续写下一章"}

	if err := applyWritingSkillRuntimePolicy(context.Background(), runtime, req); err != nil {
		t.Fatal(err)
	}
	if req.LoadedWritingSkill != nil {
		t.Fatalf("oversized preset should remain dynamic: %#v", req.LoadedWritingSkill)
	}
	if runtime.cfg.AgentSkills.IDE != nil {
		t.Fatalf("dynamic fallback must keep skill available: %#v", runtime.cfg.AgentSkills.IDE)
	}
}

func TestApplyWritingSkillRuntimePolicyKeepsWorkspaceOverrideDynamic(t *testing.T) {
	root := t.TempDir()
	builtin := filepath.Join(root, "builtin")
	workspace := filepath.Join(root, "workspace")
	writeTestSkill(t, builtin, "novel-lite", "builtin preset")
	writeTestSkill(t, filepath.Join(workspace, ".denova", "skills"), "novel-lite", "workspace preset")
	runtime := &ideChatRuntime{workspace: workspace, cfg: config.Config{
		SkillsDir:           builtin,
		DenovaDir:           filepath.Join(root, "data"),
		WritingSkillDefault: "novel-lite",
	}}
	req := &agent.ChatRequest{Message: "续写下一章"}

	if err := applyWritingSkillRuntimePolicy(context.Background(), runtime, req); err != nil {
		t.Fatal(err)
	}
	if req.LoadedWritingSkill != nil {
		t.Fatalf("workspace override must remain dynamic: %#v", req.LoadedWritingSkill)
	}
	if runtime.cfg.AgentSkills.IDE != nil {
		t.Fatalf("workspace override should stay available to skill tool: %#v", runtime.cfg.AgentSkills.IDE)
	}
}

func writeTestSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: test\nagent: ide\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
