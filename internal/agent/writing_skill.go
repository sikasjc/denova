package agent

import (
	"strings"

	"denova/config"
)

const maxInlineWritingSkillRunes = 128 * 1024

var builtinWritingPresets = map[string]bool{
	"novel-lite":     true,
	"novel-standard": true,
	"novel-heavy":    true,
}

type WritingIntentResolution struct {
	Intent              config.WritingIntent
	NeedsClassification bool
	Reason              string
}

func IsBuiltinWritingPreset(name string) bool {
	return builtinWritingPresets[strings.TrimSpace(name)]
}

func BuiltinWritingPresetNames() []string {
	return []string{"novel-lite", "novel-standard", "novel-heavy"}
}

// ResolveWritingIntentRoute handles only trusted structured signals and hard
// non-execution guards. Every other free-form user message is classified by the
// Writing fast model so keyword maintenance does not become the primary router.
func ResolveWritingIntentRoute(req ChatRequest) WritingIntentResolution {
	if len(req.ReviewFeedback) > 0 || !req.ResolvedReviewFeedback.Empty() {
		return WritingIntentResolution{Intent: config.WritingIntentReviewApplication, Reason: "structured_review_feedback"}
	}
	switch config.NormalizeWritingIntent(req.WritingIntent) {
	case config.WritingIntentProseGeneration, config.WritingIntentProseRevision, config.WritingIntentReviewApplication:
		return WritingIntentResolution{Intent: config.NormalizeWritingIntent(req.WritingIntent), Reason: "structured_execution_intent"}
	case config.WritingIntentPlanning, config.WritingIntentAnalysis:
		return WritingIntentResolution{Intent: config.NormalizeWritingIntent(req.WritingIntent), Reason: "structured_non_execution_intent"}
	}
	message := strings.ToLower(strings.TrimSpace(req.Message))
	if message == "" {
		return WritingIntentResolution{Reason: "empty_message"}
	}
	if strings.HasPrefix(message, "/") {
		return WritingIntentResolution{Intent: config.WritingIntentAnalysis, Reason: "explicit_command"}
	}
	if containsWritingIntentMarker(message, []string{
		"只讨论", "先讨论", "讨论为主", "只聊", "先聊", "暂时不写", "暂时不要写", "先不要写", "先别写", "不要写",
		"暂时不改", "暂时不要改", "先不要改", "先别改", "不要修改", "不要改写", "不要重写", "不要润色",
		"不要执行", "先不要执行", "不要落盘", "先不落盘", "只分析", "只给建议",
		"just discuss", "discussion only", "discuss first", "do not write", "don't write", "do not revise", "don't revise",
		"do not rewrite", "don't rewrite", "do not edit", "don't edit", "do not modify", "don't modify",
		"do not execute", "don't execute", "do not apply", "don't apply", "analysis only", "suggestions only",
	}) {
		return WritingIntentResolution{Intent: config.WritingIntentAnalysis, Reason: "explicit_non_execution"}
	}
	return WritingIntentResolution{NeedsClassification: true, Reason: "free_form_requires_fast_classification"}
}

// ShouldInlineWritingSkill trusts only structured or already classified intent.
func ShouldInlineWritingSkill(req ChatRequest) bool {
	switch config.NormalizeWritingIntent(req.WritingIntent) {
	case config.WritingIntentProseGeneration, config.WritingIntentProseRevision, config.WritingIntentReviewApplication:
		return true
	default:
		return len(req.ReviewFeedback) > 0 || !req.ResolvedReviewFeedback.Empty()
	}
}

func containsWritingIntentMarker(message string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func InlineWritingSkillContentAllowed(content string) bool {
	size := len([]rune(strings.TrimSpace(content)))
	return size > 0 && size <= maxInlineWritingSkillRunes
}

// ResolveWritingSkillName selects the effective Writing Skill name for this IDE
// turn without reading SKILL.md. The model decides whether to load it through
// the skill tool based on the dynamic turn hint.
func ResolveWritingSkillName(cfg *config.Config, selected string) string {
	name := strings.TrimSpace(selected)
	if name == "" && cfg != nil {
		name = strings.TrimSpace(cfg.WritingSkillDefault)
	}
	if name == "" {
		name = config.DefaultWritingSkillName
	}
	return name
}
