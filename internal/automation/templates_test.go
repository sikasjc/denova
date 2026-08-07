package automation

import (
	"strings"
	"testing"
)

func TestBuiltinTaskTemplatesProvideLocalizedWorkspaceDrafts(t *testing.T) {
	zh := BuiltinTaskTemplates("zh-CN")
	en := BuiltinTaskTemplates("en-US")
	if len(zh) != 2 || len(en) != 2 {
		t.Fatalf("template count zh=%d en=%d, want 2", len(zh), len(en))
	}

	continueWriting := templateByID(zh, TemplateContinueWriting)
	if continueWriting == nil {
		t.Fatal("continue-writing template missing")
	}
	if continueWriting.Defaults.Enabled || continueWriting.Defaults.WriteMode != WriteModeConfirmWrite || continueWriting.Defaults.WriteScope != WriteScopeFile {
		t.Fatalf("unexpected continue-writing defaults: %#v", continueWriting.Defaults)
	}
	if len(continueWriting.TargetKinds) != 1 || continueWriting.TargetKinds[0] != TargetKindWorkspace {
		t.Fatalf("continue-writing targets = %#v", continueWriting.TargetKinds)
	}
	if !strings.Contains(continueWriting.Defaults.Prompt, "续写下一章") {
		t.Fatalf("Chinese prompt not localized: %q", continueWriting.Defaults.Prompt)
	}
	for _, required := range []string{
		"若本轮生效 Writing preset，严格按其审稿、修订与最终机械验证顺序执行",
		"否则只做一次轻量自检和最多一次最小修正",
		"不额外增加审稿流水线",
		"形成最终稿后",
	} {
		if !strings.Contains(continueWriting.Defaults.Prompt, required) {
			t.Fatalf("Chinese continue-writing prompt missing workflow boundary %q: %q", required, continueWriting.Defaults.Prompt)
		}
	}

	continueWritingEnglish := templateByID(en, TemplateContinueWriting)
	if continueWritingEnglish == nil {
		t.Fatal("English continue-writing template missing")
	}
	for _, required := range []string{
		"When a Writing preset is active, follow its review, revision, and final mechanical-verification sequence",
		"Otherwise perform one lightweight check and at most one minimal correction",
		"without adding a review pipeline",
		"Once the final draft is established",
	} {
		if !strings.Contains(continueWritingEnglish.Defaults.Prompt, required) {
			t.Fatalf("English continue-writing prompt missing workflow boundary %q: %q", required, continueWritingEnglish.Defaults.Prompt)
		}
	}

	review := templateByID(en, TemplateReview)
	if review == nil {
		t.Fatal("review template missing")
	}
	if review.Defaults.Name != "Automatic Review" || !strings.Contains(review.Defaults.Prompt, "new chapters") {
		t.Fatalf("English review template not localized: %#v", review)
	}
	if len(review.Defaults.Triggers) != 1 || review.Defaults.Triggers[0].Type != TriggerTypeChapterBatch || review.Defaults.Triggers[0].ChapterBatchSize != 5 {
		t.Fatalf("unexpected review trigger defaults: %#v", review.Defaults.Triggers)
	}
}

func templateByID(templates []TaskTemplate, id string) *TaskTemplate {
	for i := range templates {
		if templates[i].ID == id {
			return &templates[i]
		}
	}
	return nil
}
