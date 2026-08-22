package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk/middlewares/skill"
)

func TestCompactWriteTodosToolKeepsTheTodoContractWithoutTheUpstreamTutorial(t *testing.T) {
	base, err := newCompactWriteTodosTool()
	if err != nil {
		t.Fatal(err)
	}
	info, err := base.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "write_todos" || !strings.Contains(info.Desc, "待办") || len([]rune(info.Desc)) > 80 {
		t.Fatalf("unexpected compact todo schema: %#v", info)
	}
}

func TestCompactSkillInstructionsKeepAvailableSkillsBounded(t *testing.T) {
	description := compactSkillToolDescription(context.Background(), []skill.FrontMatter{{
		Name:        "novel-standard",
		Description: strings.Repeat("用于正式章节创作。", 80),
	}})
	if !strings.Contains(description, "novel-standard") || len([]rune(description)) > 280 {
		t.Fatalf("unexpected compact skill schema: %q", description)
	}
	system := compactSkillSystemPrompt(context.Background(), "skill")
	if !strings.Contains(system, "skill") || !strings.Contains(system, "已内联") {
		t.Fatalf("unexpected compact skill system instruction: %q", system)
	}
}
