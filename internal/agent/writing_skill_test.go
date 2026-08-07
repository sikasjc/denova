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

func TestShouldInlineWritingSkillTrustsOnlyStructuredIntentAndReviewFeedback(t *testing.T) {
	for _, req := range []ChatRequest{
		{Message: "把这章写得更有张力", WritingIntent: config.WritingIntentProseRevision},
		{Message: "请处理审阅意见", ReviewFeedback: ReviewFeedbackRefs{{ReviewThreadID: "thread-1", CommentIDs: []string{"comment-1"}}}},
	} {
		if !ShouldInlineWritingSkill(req) {
			t.Fatalf("expected inline writing skill for %#v", req)
		}
	}
	for _, req := range []ChatRequest{
		{Message: "续写下一章"},
		{Message: "请修改章节里的这段对白"},
		{Message: "Please revise this scene."},
		{Message: "帮我分析 progress.md 是否有问题"},
		{Message: "讨论一下人物关系"},
		{Message: "讨论一下改写方案"},
		{Message: "这个场景要不要续写得更紧张？"},
		{Message: "你觉得可以写一些什么？"},
		{Message: "先别写，聊聊怎么修改这章"},
		{Message: "/rewrite chapters/ch01.md"},
		{Message: "创作方向要不要调整", WritingIntent: config.WritingIntentPlanning},
	} {
		if ShouldInlineWritingSkill(req) {
			t.Fatalf("expected dynamic/no writing skill for %#v", req)
		}
	}
}

func TestResolveWritingIntentRouteClassifiesEveryFreeFormMessage(t *testing.T) {
	tests := []struct {
		name           string
		message        string
		intent         config.WritingIntent
		classification bool
	}{
		{name: "explicit generation", message: "请写下一章", classification: true},
		{name: "explicit revision", message: "请润色当前章节", classification: true},
		{name: "discussion", message: "讨论一下改写方案", classification: true},
		{name: "planning", message: "生成前三阶段的详细大纲", classification: true},
		{name: "free-form revision request", message: "把这章写得更有张力", classification: true},
		{name: "unrelated question", message: "这个角色现在多大？", classification: true},
		{name: "hard negative", message: "先别写，只讨论方向", intent: config.WritingIntentAnalysis},
		{name: "slash command", message: "/rewrite chapters/ch01.md", intent: config.WritingIntentAnalysis},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveWritingIntentRoute(ChatRequest{Message: tt.message})
			if got.Intent != tt.intent || got.NeedsClassification != tt.classification {
				t.Fatalf("resolution = %#v, want intent=%q classification=%v", got, tt.intent, tt.classification)
			}
		})
	}
}

func TestShouldInlineWritingSkillTrustsStructuredIntentBeforeKeywordFallback(t *testing.T) {
	if !ShouldInlineWritingSkill(ChatRequest{
		Message:       "完成第十章",
		WritingIntent: config.WritingIntentProseGeneration,
	}) {
		t.Fatal("structured prose_generation intent should inline without keyword matching")
	}
	if ShouldInlineWritingSkill(ChatRequest{
		Message:       "讨论一下改写方案",
		WritingIntent: config.WritingIntentAnalysis,
	}) {
		t.Fatal("structured analysis intent should suppress broad rewrite keyword matching")
	}
}

func TestParseWritingIntentClassificationIsConservative(t *testing.T) {
	message := "请润色当前章节"
	high, err := parseWritingIntentClassification(`{"intent":"prose_revision","execute_now":true,"confidence":"high","evidence":"请润色当前章节","reason":"明确要求修改正文"}`)
	if err != nil || !high.AllowsExecution(message) {
		t.Fatalf("high-confidence revision should allow execution: %#v err=%v", high, err)
	}
	medium, err := parseWritingIntentClassification(`{"intent":"prose_generation","execute_now":true,"confidence":"medium","evidence":"写下一章","reason":"可能只是讨论"}`)
	if err != nil || medium.AllowsExecution("写下一章") {
		t.Fatalf("medium-confidence generation must remain non-execution: %#v err=%v", medium, err)
	}
	missingEvidence, err := parseWritingIntentClassification(`{"intent":"prose_revision","execute_now":true,"confidence":"high","evidence":"修改正文","reason":"明确要求修改正文"}`)
	if err != nil || missingEvidence.AllowsExecution(message) {
		t.Fatalf("evidence absent from the current message must block execution: %#v err=%v", missingEvidence, err)
	}
	discussion, err := parseWritingIntentClassification(`{"intent":"discussion","execute_now":false,"confidence":"high","evidence":"","reason":"询问建议"}`)
	if err != nil || discussion.Intent != config.WritingIntentAnalysis || discussion.AllowsExecution("讨论一下") {
		t.Fatalf("discussion classification mismatch: %#v err=%v", discussion, err)
	}
}

func TestWritingIntentClassifierUsesFastProfileWithoutCustomPrompt(t *testing.T) {
	cfg := &config.Config{
		WritingComputeFastProfileID: "flash",
		AgentModels: config.AgentModelSettings{
			Default:   config.AgentModelOverride{ProfileID: "default", ReasoningEffort: "high"},
			ToolAgent: config.AgentModelOverride{ProfileID: "custom-tool", ReasoningEffort: "medium"},
		},
		AgentPrompts: config.AgentPromptSettings{
			Default:   config.AgentPromptOverride{SystemPrompt: "default custom prompt"},
			ToolAgent: config.AgentPromptOverride{FlowPrompt: "tool custom flow", SystemPrompt: "tool custom prompt"},
		},
	}
	classifierCfg := writingIntentClassifierConfig(cfg)
	if classifierCfg.AgentModels.Default != (config.AgentModelOverride{}) {
		t.Fatalf("classifier should clear default model override: %#v", classifierCfg.AgentModels.Default)
	}
	if classifierCfg.AgentModels.ToolAgent.ProfileID != "flash" ||
		classifierCfg.AgentModels.ToolAgent.EnableThinking == nil ||
		*classifierCfg.AgentModels.ToolAgent.EnableThinking ||
		classifierCfg.AgentModels.ToolAgent.ReasoningEffort != "" {
		t.Fatalf("classifier model override = %#v", classifierCfg.AgentModels.ToolAgent)
	}
	if classifierCfg.AgentPrompts != (config.AgentPromptSettings{}) {
		t.Fatalf("classifier should ignore editable prompts: %#v", classifierCfg.AgentPrompts)
	}
}

func TestInlineWritingSkillContentLimit(t *testing.T) {
	if !InlineWritingSkillContentAllowed("short preset") {
		t.Fatal("small built-in preset should be allowed")
	}
	if InlineWritingSkillContentAllowed(strings.Repeat("x", maxInlineWritingSkillRunes+1)) {
		t.Fatal("oversized built-in preset should fall back to dynamic loading")
	}
}
