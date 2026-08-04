package agent

import (
	"strings"

	"denova/config"
)

var builtinWritingPresets = map[string]bool{
	"novel-lite":     true,
	"novel-standard": true,
	"novel-heavy":    true,
}

func IsBuiltinWritingPreset(name string) bool {
	return builtinWritingPresets[strings.TrimSpace(name)]
}

func BuiltinWritingPresetNames() []string {
	return []string{"novel-lite", "novel-standard", "novel-heavy"}
}

// ShouldInlineWritingSkill keeps the fast path conservative: obvious prose
// generation/revision requests and structured review feedback can skip the
// skill-tool round trip, while general analysis still uses progressive
// disclosure and lets the model decide whether the selected Skill is needed.
func ShouldInlineWritingSkill(req ChatRequest) bool {
	if len(req.ReviewFeedback) > 0 || !req.ResolvedReviewFeedback.Empty() {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(req.Message))
	if message == "" {
		return false
	}
	if strings.HasPrefix(message, "/") {
		return false
	}
	for _, marker := range []string{
		"续写", "重写", "改写", "修订", "润色", "扩写", "缩写", "创作",
		"写下一章", "写一章", "写这个场景", "写一个场景", "写一段", "写这段", "修改正文", "修改章节",
		"修改这段", "改一下这段", "调整对白", "调整对话", "处理审阅意见", "应用审阅意见",
		"rewrite", "revise", "polish this", "expand this", "shorten this",
		"write a chapter", "write this chapter", "write a scene", "write this scene",
		"continue the story", "continue writing", "draft a chapter", "draft a scene", "apply the review",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
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
