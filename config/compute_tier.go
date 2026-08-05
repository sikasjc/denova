package config

import "strings"

// 写作 SubAgent 算力档位（Writing Compute Tier）
//
// 档位是介于"父 Agent 继承"与"逐 SubAgent 显式覆盖"之间的一层解析：它按 SubAgent
// 的 ComputeRole（正文 / 推理 / 检查）而非脆弱的稳定 ID，把写作管线的各阶段整体
// 映射到不同的模型 profile 与思考开关，从而让作者用一个开关在"质量优先"和"更省更快"
// 之间切换。Auto 档位中显式 SubAgentConfig.Model 优先于档位；manual 则强制全部写作
// SubAgent 跟随当前主模型，确保“指定模型”不会被旧的逐 SubAgent 覆盖拆散。
//
// 档位只作用于写作（IDE）管线；游戏模式的 choreographer 仍继承 interactive_story，
// 边界清晰、互不影响。
type WritingComputeTier string

const (
	// WritingComputeTierManual 指定模型：全部阶段继承写作主 Agent 当前选择的模型与思考设置。
	// 该档位与 quality 的解析矩阵相同，但保留独立 wire 值，让统一选择器刷新后仍能区分
	// “作者明确指定单一模型”和“Auto-质量优先”两种选择意图。
	WritingComputeTierManual WritingComputeTier = "manual"
	// WritingComputeTierQuality 质量优先：全部阶段使用 pro + 思考，等价于历史行为。
	WritingComputeTierQuality WritingComputeTier = "quality"
	// WritingComputeTierBalanced 平衡（默认）：正文阶段用 pro，推理/检查阶段下放到 flash。
	WritingComputeTierBalanced WritingComputeTier = "balanced"
	// WritingComputeTierSpeed 速度优先：全部阶段用 flash，仅正文阶段保留思考。
	WritingComputeTierSpeed WritingComputeTier = "speed"
)

// DefaultWritingComputeTier 是内置默认档位。开箱即用时正文走 pro、其余走 flash+思考，
// 在保证文笔质量的同时降低延迟与成本。
const DefaultWritingComputeTier = WritingComputeTierBalanced

// DefaultFastModelProfileID 是档位快速阶段默认引用的模型 profile id。作者可在设置里
// 把它改成任意 profile（见 Config.WritingComputeFastProfileID）。当选中的 profile 不存在
// 时，ResolveAgentModel 会自动回退到 "default"，档位安全降级为 pro，不会报错。
const DefaultFastModelProfileID = "flash"

// NormalizeFastModelProfileID 归一档位快速模型 profile id；空值回退到内置默认 "flash"。
func NormalizeFastModelProfileID(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return DefaultFastModelProfileID
	}
	return trimmed
}

// ComputeRole 标注一个写作 SubAgent 在管线中的认知负载类别，供档位按类映射模型。
// 空字符串表示不参与档位映射（完全继承父 Agent）。
type ComputeRole string

const (
	// ComputeRoleProse 正文角色：直接决定成稿文笔质量（writer / fixer），各档都用 pro。
	ComputeRoleProse ComputeRole = "prose"
	// ComputeRoleReasoning 推理角色：结构化判断与多拍推演，思考比文笔更关键
	// （reviewer / context-planner / choreographer / intimacy-choreographer）。
	ComputeRoleReasoning ComputeRole = "reasoning"
	// ComputeRoleMechanical 检查角色：低负载的判定或状态抽取（final-gate / memory-patcher）。
	// 值沿用 "mechanical" 作为稳定 wire 值；面向用户的展示名为「检查」。
	ComputeRoleMechanical ComputeRole = "mechanical"
)

// NormalizeWritingComputeTier 归一档位值；未知或空值回退到内置默认档位。
func NormalizeWritingComputeTier(tier WritingComputeTier) WritingComputeTier {
	switch WritingComputeTier(strings.TrimSpace(string(tier))) {
	case WritingComputeTierManual:
		return WritingComputeTierManual
	case WritingComputeTierQuality:
		return WritingComputeTierQuality
	case WritingComputeTierBalanced:
		return WritingComputeTierBalanced
	case WritingComputeTierSpeed:
		return WritingComputeTierSpeed
	default:
		return DefaultWritingComputeTier
	}
}

// NormalizeComputeRole 归一 role 值；未知或空值返回空 role（不参与档位映射）。
func NormalizeComputeRole(role ComputeRole) ComputeRole {
	switch ComputeRole(strings.TrimSpace(string(role))) {
	case ComputeRoleProse:
		return ComputeRoleProse
	case ComputeRoleReasoning:
		return ComputeRoleReasoning
	case ComputeRoleMechanical:
		return ComputeRoleMechanical
	default:
		return ""
	}
}

// sanitizeWritingComputeTierLayer 归一单个配置层的档位值：空值保持为空（表示继承上层，
// 不能强制成默认，否则会破坏分层语义），未知值丢弃为空，合法值原样保留。
// effective 层最终由 NormalizeWritingComputeTier 兜底成默认档位。
func sanitizeWritingComputeTierLayer(tier WritingComputeTier) WritingComputeTier {
	switch WritingComputeTier(strings.TrimSpace(string(tier))) {
	case WritingComputeTierManual:
		return WritingComputeTierManual
	case WritingComputeTierQuality:
		return WritingComputeTierQuality
	case WritingComputeTierBalanced:
		return WritingComputeTierBalanced
	case WritingComputeTierSpeed:
		return WritingComputeTierSpeed
	default:
		return ""
	}
}

// writingComputeTierPlan 描述某个档位对某个 role 的调整。Fast=true 表示该阶段下放到
// 快速模型 profile（实际 id 由 Config.WritingComputeFastProfileID 决定，可配置）；
// Fast=false 表示沿用父继承的模型（不换 profile）。EnableThinking=nil 表示沿用父继承。
type writingComputeTierPlan struct {
	Fast           bool
	EnableThinking *bool
}

// writingComputeTierMatrix 是档位 × role 的静态映射表（Go 权威定义）。
//
// - manual：所有 role 强制沿用父继承；ResolveSubAgentModel 会跳过逐 SubAgent 覆盖。
// - quality：所有 role 都沿用父继承（IDE 默认 pro + 思考），等价历史行为。
// - balanced：正文沿用父 pro；推理/检查下放到快速模型，推理保留思考、检查关思考。
// - speed：所有 role 都用快速模型；仅正文保留思考，其余关思考以最快出稿。
var writingComputeTierMatrix = map[WritingComputeTier]map[ComputeRole]writingComputeTierPlan{
	WritingComputeTierManual: {
		ComputeRoleProse:      {},
		ComputeRoleReasoning:  {},
		ComputeRoleMechanical: {},
	},
	WritingComputeTierQuality: {
		ComputeRoleProse:      {},
		ComputeRoleReasoning:  {},
		ComputeRoleMechanical: {},
	},
	WritingComputeTierBalanced: {
		ComputeRoleProse:      {},
		ComputeRoleReasoning:  {Fast: true, EnableThinking: boolPtr(true)},
		ComputeRoleMechanical: {Fast: true, EnableThinking: boolPtr(false)},
	},
	WritingComputeTierSpeed: {
		ComputeRoleProse:      {Fast: true, EnableThinking: boolPtr(true)},
		ComputeRoleReasoning:  {Fast: true, EnableThinking: boolPtr(false)},
		ComputeRoleMechanical: {Fast: true, EnableThinking: boolPtr(false)},
	},
}

// writingComputeTierOverride 返回指定档位对指定 role 的模型覆盖；无适用调整时返回零值
// AgentModelOverride（等价"不改，沿用父继承"）。fastProfileID 是快速阶段实际引用的
// profile id（已归一，通常来自 Config.WritingComputeFastProfileID）。
func writingComputeTierOverride(tier WritingComputeTier, role ComputeRole, fastProfileID string) AgentModelOverride {
	role = NormalizeComputeRole(role)
	if role == "" {
		return AgentModelOverride{}
	}
	plan, ok := writingComputeTierMatrix[NormalizeWritingComputeTier(tier)][role]
	if !ok {
		return AgentModelOverride{}
	}
	profileID := ""
	if plan.Fast {
		profileID = NormalizeFastModelProfileID(fastProfileID)
	}
	return AgentModelOverride{
		ProfileID:      profileID,
		EnableThinking: plan.EnableThinking,
	}
}

// WritingComputeTierRow 是导出给前端展示与选择的单个档位描述。
type WritingComputeTierRow struct {
	ID string `json:"id"`
	// Roles 映射每个 role 在该档位下的模型 profile 提示（"" 表示继承父级 pro）与思考开关，
	// 供前端渲染"阶段 → 模型"汇总，无需前端重复档位逻辑。
	Roles map[ComputeRole]WritingComputeTierRoleRow `json:"roles"`
}

// WritingComputeTierRoleRow 描述某档位下某 role 的生效模型与思考状态。
// ProfileID 为空表示沿用父继承的模型（IDE 默认 pro）。
// EnableThinking 为 nil 表示沿用父继承的思考开关。
type WritingComputeTierRoleRow struct {
	ProfileID      string `json:"profile_id,omitempty"`
	EnableThinking *bool  `json:"enable_thinking,omitempty"`
}

// WritingComputeTiers 是全部可选档位，按从质量到速度排序，供前端选择器与汇总展示复用。
func WritingComputeTiers() []WritingComputeTier {
	return []WritingComputeTier{
		WritingComputeTierManual,
		WritingComputeTierQuality,
		WritingComputeTierBalanced,
		WritingComputeTierSpeed,
	}
}

// WritingComputeTierRows 导出档位 × role 静态表给前端，使"阶段 → 模型/思考"展示与后端
// 解析共用同一份权威数据，避免前后端逻辑漂移。fastProfileID 是快速阶段实际引用的
// profile id（可配置），会体现在返回的 ProfileID 上，让前端展示与实际生效一致。
func WritingComputeTierRows(fastProfileID string) []WritingComputeTierRow {
	roles := []ComputeRole{ComputeRoleProse, ComputeRoleReasoning, ComputeRoleMechanical}
	rows := make([]WritingComputeTierRow, 0, len(WritingComputeTiers()))
	for _, tier := range WritingComputeTiers() {
		roleRows := make(map[ComputeRole]WritingComputeTierRoleRow, len(roles))
		for _, role := range roles {
			override := writingComputeTierOverride(tier, role, fastProfileID)
			roleRows[role] = WritingComputeTierRoleRow{
				ProfileID:      override.ProfileID,
				EnableThinking: override.EnableThinking,
			}
		}
		rows = append(rows, WritingComputeTierRow{ID: string(tier), Roles: roleRows})
	}
	return rows
}
