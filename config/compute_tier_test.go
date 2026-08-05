package config

import "testing"

// tierTestConfig 构造一个带 pro(default) 与 flash profile、IDE 默认 pro+思考的配置，
// 用于验证写作算力档位按 ComputeRole 映射模型。
func tierTestConfig(tier WritingComputeTier) *Config {
	return &Config{
		OpenAIModel: "deepseek-v4-pro",
		ModelProfiles: []ModelProfileSettings{
			{ID: "default", OpenAIModel: "deepseek-v4-pro"},
			{ID: "flash", OpenAIModel: "deepseek-v4-flash"},
		},
		AgentModels: AgentModelSettings{
			IDE: AgentModelOverride{EnableThinking: boolPtr(true)},
		},
		WritingComputeTier: tier,
	}
}

func subAgent(id string, role ComputeRole) SubAgentConfig {
	return SubAgentConfig{ID: id, Parents: []string{AgentKindIDE}, ComputeRole: role}
}

func TestResolveSubAgentModelQualityTierKeepsProEverywhere(t *testing.T) {
	cfg := tierTestConfig(WritingComputeTierQuality)
	for _, role := range []ComputeRole{ComputeRoleProse, ComputeRoleReasoning, ComputeRoleMechanical} {
		resolved := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("s", role))
		if resolved.OpenAIModel != "deepseek-v4-pro" {
			t.Fatalf("quality role=%s model = %q, want pro", role, resolved.OpenAIModel)
		}
		if resolved.EnableThinking == nil || !*resolved.EnableThinking {
			t.Fatalf("quality role=%s should keep thinking on", role)
		}
	}
}

func TestResolveSubAgentModelBalancedTierMapsByRole(t *testing.T) {
	cfg := tierTestConfig(WritingComputeTierBalanced)

	prose := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("writer", ComputeRoleProse))
	if prose.OpenAIModel != "deepseek-v4-pro" || prose.EnableThinking == nil || !*prose.EnableThinking {
		t.Fatalf("balanced prose should stay pro+thinking: %#v", prose)
	}

	reasoning := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("reviewer", ComputeRoleReasoning))
	if reasoning.OpenAIModel != "deepseek-v4-flash" {
		t.Fatalf("balanced reasoning model = %q, want flash", reasoning.OpenAIModel)
	}
	if reasoning.EnableThinking == nil || !*reasoning.EnableThinking {
		t.Fatalf("balanced reasoning should keep thinking on: %#v", reasoning)
	}

	mechanical := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("final-gate", ComputeRoleMechanical))
	if mechanical.OpenAIModel != "deepseek-v4-flash" {
		t.Fatalf("balanced mechanical model = %q, want flash", mechanical.OpenAIModel)
	}
	if mechanical.EnableThinking == nil || *mechanical.EnableThinking {
		t.Fatalf("balanced mechanical should turn thinking off: %#v", mechanical)
	}
}

func TestResolveSubAgentModelSpeedTierUsesFlashEverywhere(t *testing.T) {
	cfg := tierTestConfig(WritingComputeTierSpeed)
	for _, role := range []ComputeRole{ComputeRoleProse, ComputeRoleReasoning, ComputeRoleMechanical} {
		resolved := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("s", role))
		if resolved.OpenAIModel != "deepseek-v4-flash" {
			t.Fatalf("speed role=%s model = %q, want flash", role, resolved.OpenAIModel)
		}
	}
	prose := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("writer", ComputeRoleProse))
	if prose.EnableThinking == nil || !*prose.EnableThinking {
		t.Fatalf("speed prose should keep thinking on: %#v", prose)
	}
	mechanical := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("gate", ComputeRoleMechanical))
	if mechanical.EnableThinking == nil || *mechanical.EnableThinking {
		t.Fatalf("speed mechanical should turn thinking off: %#v", mechanical)
	}
}

func TestResolveSubAgentModelExplicitOverrideWinsOverTier(t *testing.T) {
	cfg := tierTestConfig(WritingComputeTierSpeed)
	// speed 会把 reasoning 下放到 flash，但显式覆盖回 default 必须优先。
	sub := subAgent("reviewer", ComputeRoleReasoning)
	sub.Model = AgentModelOverride{ProfileID: "default", EnableThinking: boolPtr(true)}
	resolved := ResolveSubAgentModel(cfg, AgentKindIDE, sub)
	if resolved.OpenAIModel != "deepseek-v4-pro" {
		t.Fatalf("explicit override model = %q, want pro (override wins over tier)", resolved.OpenAIModel)
	}
	if resolved.EnableThinking == nil || !*resolved.EnableThinking {
		t.Fatalf("explicit thinking override should win: %#v", resolved)
	}
}

func TestResolveSubAgentModelTierIgnoredForNonIDEParent(t *testing.T) {
	cfg := tierTestConfig(WritingComputeTierSpeed)
	// 游戏模式 choreographer 继承 interactive_story，写作档位不得影响它。
	resolved := ResolveSubAgentModel(cfg, AgentKindInteractiveStory, subAgent("choreographer", ComputeRoleReasoning))
	if resolved.OpenAIModel != "deepseek-v4-pro" {
		t.Fatalf("non-IDE parent model = %q, want inherited pro (tier must not apply)", resolved.OpenAIModel)
	}
}

func TestResolveSubAgentModelFallsBackToProWhenFlashMissing(t *testing.T) {
	// 未配置 flash profile 时，balanced 的 reasoning 请求 flash 应回退到 default(pro)。
	cfg := &Config{
		OpenAIModel: "deepseek-v4-pro",
		AgentModels: AgentModelSettings{
			IDE: AgentModelOverride{EnableThinking: boolPtr(true)},
		},
		WritingComputeTier: WritingComputeTierBalanced,
	}
	resolved := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("reviewer", ComputeRoleReasoning))
	if resolved.OpenAIModel != "deepseek-v4-pro" {
		t.Fatalf("missing flash should fall back to pro, got %q", resolved.OpenAIModel)
	}
	if resolved.ProfileID != "default" {
		t.Fatalf("missing flash should fall back to default profile, got %q", resolved.ProfileID)
	}}

func TestResolveSubAgentModelEmptyRoleFullyInherits(t *testing.T) {
	cfg := tierTestConfig(WritingComputeTierSpeed)
	// 无 ComputeRole 的 SubAgent 不参与档位映射，完全继承父级 pro+思考。
	resolved := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("custom", ""))
	if resolved.OpenAIModel != "deepseek-v4-pro" {
		t.Fatalf("role-less subagent model = %q, want inherited pro", resolved.OpenAIModel)
	}
	if resolved.EnableThinking == nil || !*resolved.EnableThinking {
		t.Fatalf("role-less subagent should inherit thinking on: %#v", resolved)
	}
}

func TestResolveSubAgentModelUsesConfiguredFastProfile(t *testing.T) {
	// 作者把档位快速模型换成自定义 profile "myfast" 时，balanced 的推理/检查阶段应走它，
	// 而不是内置 flash；正文仍走 pro。
	cfg := &Config{
		OpenAIModel: "deepseek-v4-pro",
		ModelProfiles: []ModelProfileSettings{
			{ID: "default", OpenAIModel: "deepseek-v4-pro"},
			{ID: "flash", OpenAIModel: "deepseek-v4-flash"},
			{ID: "myfast", OpenAIModel: "some-other-fast"},
		},
		AgentModels: AgentModelSettings{
			IDE: AgentModelOverride{EnableThinking: boolPtr(true)},
		},
		WritingComputeTier:          WritingComputeTierBalanced,
		WritingComputeFastProfileID: "myfast",
	}
	reasoning := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("reviewer", ComputeRoleReasoning))
	if reasoning.ProfileID != "myfast" || reasoning.OpenAIModel != "some-other-fast" {
		t.Fatalf("reasoning should use configured fast profile, got %#v", reasoning)
	}
	prose := ResolveSubAgentModel(cfg, AgentKindIDE, subAgent("writer", ComputeRoleProse))
	if prose.OpenAIModel != "deepseek-v4-pro" {
		t.Fatalf("prose should stay pro even with a custom fast profile, got %q", prose.OpenAIModel)
	}
}

func TestWritingComputeTierRowsReflectConfiguredFastProfile(t *testing.T) {
	rows := WritingComputeTierRows("myfast")
	for _, row := range rows {
		if row.ID != string(WritingComputeTierBalanced) {
			continue
		}
		if got := row.Roles[ComputeRoleReasoning].ProfileID; got != "myfast" {
			t.Fatalf("balanced reasoning row should show configured fast profile, got %q", got)
		}
		if got := row.Roles[ComputeRoleProse].ProfileID; got != "" {
			t.Fatalf("balanced prose row should still inherit (empty), got %q", got)
		}
	}
	// 空 fast id 走内置默认 flash。
	rows = WritingComputeTierRows("")
	for _, row := range rows {
		if row.ID != string(WritingComputeTierSpeed) {
			continue
		}
		if got := row.Roles[ComputeRoleProse].ProfileID; got != DefaultFastModelProfileID {
			t.Fatalf("empty fast id should default to flash, got %q", got)
		}
	}
}

func TestNormalizeWritingComputeTierDefaultsToBalanced(t *testing.T) {
	if got := NormalizeWritingComputeTier(""); got != WritingComputeTierBalanced {
		t.Fatalf("empty tier = %q, want balanced default", got)
	}
	if got := NormalizeWritingComputeTier("nonsense"); got != WritingComputeTierBalanced {
		t.Fatalf("unknown tier = %q, want balanced default", got)
	}
	if got := NormalizeWritingComputeTier(WritingComputeTierSpeed); got != WritingComputeTierSpeed {
		t.Fatalf("valid tier should pass through, got %q", got)
	}
}

func TestWritingComputeTierRowsExposeStageMapping(t *testing.T) {
	rows := WritingComputeTierRows(DefaultFastModelProfileID)
	byID := map[string]WritingComputeTierRow{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	balanced, ok := byID[string(WritingComputeTierBalanced)]
	if !ok {
		t.Fatalf("balanced tier row missing: %#v", byID)
	}
	if balanced.Roles[ComputeRoleProse].ProfileID != "" {
		t.Fatalf("balanced prose should inherit (empty profile), got %q", balanced.Roles[ComputeRoleProse].ProfileID)
	}
	if balanced.Roles[ComputeRoleReasoning].ProfileID != DefaultFastModelProfileID {
		t.Fatalf("balanced reasoning should map to flash, got %q", balanced.Roles[ComputeRoleReasoning].ProfileID)
	}
	if think := balanced.Roles[ComputeRoleMechanical].EnableThinking; think == nil || *think {
		t.Fatalf("balanced mechanical thinking should be explicit off: %#v", balanced.Roles[ComputeRoleMechanical])
	}
}

func TestWritingComputeTierLayeringOverridesAndSanitizes(t *testing.T) {
	// 用户层显式设置应覆盖默认层。
	def := DefaultSettings()
	user := Settings{WritingComputeTier: WritingComputeTierSpeed}
	if got := Merge(def, user).WritingComputeTier; got != WritingComputeTierSpeed {
		t.Fatalf("user tier should override default, got %q", got)
	}
	// 空的子层不得清掉父层（分层继承语义）。
	if got := Merge(def, Settings{}).WritingComputeTier; got != DefaultWritingComputeTier {
		t.Fatalf("empty child tier should inherit default, got %q", got)
	}
	// sanitize：空值保持为空（继承上层），未知值丢弃为空，合法值保留。
	if got := sanitizeWritingComputeTierLayer(""); got != "" {
		t.Fatalf("empty tier layer should stay empty, got %q", got)
	}
	if got := sanitizeWritingComputeTierLayer("nonsense"); got != "" {
		t.Fatalf("unknown tier layer should drop to empty, got %q", got)
	}
	if got := sanitizeWritingComputeTierLayer(WritingComputeTierQuality); got != WritingComputeTierQuality {
		t.Fatalf("valid tier layer should pass through, got %q", got)
	}
}
