package agent

import (
	"strings"
	"testing"

	"denova/config"
)

func TestResolveWritingSkillNameDefaultsAndSelection(t *testing.T) {
	if got := ResolveWritingSkillName(&config.Config{WritingSkillDefault: "novel-heavy"}, ""); got != "novel-heavy" {
		t.Fatalf("default writing skill = %s, want novel-heavy", got)
	}
	if got := ResolveWritingSkillName(&config.Config{WritingSkillDefault: "novel-heavy"}, "slow-burn"); got != "slow-burn" {
		t.Fatalf("selected writing skill = %s, want slow-burn", got)
	}
	if got := ResolveWritingSkillName(&config.Config{}, ""); got != config.DefaultWritingSkillName {
		t.Fatalf("fallback writing skill = %s, want %s", got, config.DefaultWritingSkillName)
	}
}

func TestShouldInlineWritingSkillRecognizesWritingAndReviewTurns(t *testing.T) {
	for _, req := range []ChatRequest{
		{Message: "续写下一章"},
		{Message: "请修改章节里的这段对白"},
		{Message: "Please revise this scene."},
		{Message: "请处理审阅意见", ReviewFeedback: ReviewFeedbackRefs{{ReviewThreadID: "thread-1", CommentIDs: []string{"comment-1"}}}},
	} {
		if !ShouldInlineWritingSkill(req) {
			t.Fatalf("expected inline writing skill for %#v", req)
		}
	}
	for _, req := range []ChatRequest{
		{Message: "帮我分析 progress.md 是否有问题"},
		{Message: "讨论一下人物关系"},
		{Message: "/rewrite chapters/ch01.md"},
	} {
		if ShouldInlineWritingSkill(req) {
			t.Fatalf("expected dynamic/no writing skill for %#v", req)
		}
	}
}

func TestComposeAgentInputAddsWritingSkillLoadHintWithoutSkillBody(t *testing.T) {
	composition := composeAgentInput(ChatRequest{
		Message:      "帮我分析一下 progress.md 有没有问题",
		WritingSkill: "novel-standard",
	}, nil, nil, DefaultLoopPolicy())

	for _, want := range []string{"Writing Skill 按需加载提示", "当前创作 Agent 选中的 Writing Skill 是 `novel-standard`", "当前 Agent 已启用 `skill` 工具", "调用 `skill` 工具加载 `novel-standard`", "不要假装已经读取了该 Skill 的完整说明", "不存在单独的 `writing_scope` 字段"} {
		if !strings.Contains(composition.AgentMessage, want) {
			t.Fatalf("writing skill hint missing %q:\n%s", want, composition.AgentMessage)
		}
	}
	for _, notWant := range []string{"```markdown", "SKILL.md 是本轮 IDE 创作 Agent 必须遵循"} {
		if strings.Contains(composition.AgentMessage, notWant) {
			t.Fatalf("writing skill body should not be injected, found %q:\n%s", notWant, composition.AgentMessage)
		}
	}
}

func TestComposeAgentInputUsesPreloadedBuiltinWritingSkillWithoutDynamicHint(t *testing.T) {
	composition := composeAgentInput(ChatRequest{
		Message:      "续写下一章",
		WritingSkill: "novel-lite",
		LoadedWritingSkill: &LoadedWritingSkill{
			Name:          "novel-lite",
			Description:   "fast writing",
			Content:       "# novel-lite\n\nmain agent -> final output",
			BaseDirectory: "/app/skills/novel-lite",
		},
	}, nil, nil, DefaultLoopPolicy())

	for _, want := range []string{
		"已加载的内置 Writing Skill",
		"不要再调用 `skill` 工具加载同名 Skill",
		`<writing_skill name="novel-lite">`,
		"main agent -> final output",
	} {
		if !strings.Contains(composition.AgentMessage, want) {
			t.Fatalf("preloaded writing skill missing %q:\n%s", want, composition.AgentMessage)
		}
	}
	if strings.Contains(composition.AgentMessage, "Writing Skill 按需加载提示") {
		t.Fatalf("preloaded writing skill should not include dynamic load hint:\n%s", composition.AgentMessage)
	}
}
