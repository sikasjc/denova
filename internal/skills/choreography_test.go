package skills

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"denova/config"
)

func TestBuiltInChoreographySkillsRouteToMatchingSpecialists(t *testing.T) {
	ctx := context.Background()
	skillsRoot := filepath.Join("..", "..", "skills")
	dirs := []Directory{{Scope: ScopeBuiltin, Path: skillsRoot}}
	defaults := config.DefaultChoreographySubAgents()
	defaultByID := make(map[string]config.SubAgentConfig, len(defaults))
	for _, sub := range defaults {
		defaultByID[sub.ID] = sub
	}

	for skillName, specialistID := range map[string]string{
		"action-choreography":   "choreographer",
		"intimacy-choreography": "intimacy-choreographer",
	} {
		t.Run(skillName, func(t *testing.T) {
			specialist, ok := defaultByID[specialistID]
			if !ok {
				t.Fatalf("missing built-in specialist %q", specialistID)
			}
			if !config.SubAgentAllowedForParent(specialist, config.AgentKindIDE) ||
				!config.SubAgentAllowedForParent(specialist, config.AgentKindInteractiveStory) {
				t.Fatalf("specialist %q is not available in both parent modes: %#v", specialistID, specialist)
			}

			parentSkill, err := NewAgentBackend(dirs, config.AgentKindIDE, nil).
				WithDelegates(map[string]bool{specialistID: true}).
				Get(ctx, skillName)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(parentSkill.Content, "subagent_type: "+specialistID) {
				t.Fatalf("parent router does not target %q:\n%s", specialistID, parentSkill.Content)
			}

			specialistSkill, err := NewAgentBackend(dirs, specialistID, nil).Get(ctx, skillName)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(specialistSkill.Content, "[STAGE]") || !strings.Contains(specialistSkill.Content, "[BEATS]") {
				t.Fatalf("specialist %q did not receive the full choreography method", specialistID)
			}
		})
	}
}
