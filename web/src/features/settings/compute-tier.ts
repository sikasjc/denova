import type { ComputeRole, Settings, WritingComputeTierRoleRow, WritingComputeTierRow } from './types'

// 写作算力档位（Writing Compute Tier）前端常量。
// 档位本身是固定的三档（后端 config.WritingComputeTiers()），因此这里静态列出；
// 每档下各阶段实际生效的模型/思考来自后端导出的 layered.writing_compute_tiers，
// 前端不重复档位映射逻辑。
export const WRITING_COMPUTE_TIERS = ['quality', 'balanced', 'speed'] as const
export type WritingComputeTier = (typeof WRITING_COMPUTE_TIERS)[number]

export const DEFAULT_WRITING_COMPUTE_TIER: WritingComputeTier = 'balanced'

// DEFAULT_FAST_MODEL_PROFILE_ID 是档位快速阶段默认引用的 profile id（后端 DefaultFastModelProfileID）。
export const DEFAULT_FAST_MODEL_PROFILE_ID = 'flash'

// COMPUTE_ROLES 是展示"阶段 → 模型/思考"时的稳定 role 顺序。
export const COMPUTE_ROLES: ComputeRole[] = ['prose', 'reasoning', 'mechanical']

export function normalizeWritingComputeTier(value?: string | null): WritingComputeTier {
  const trimmed = (value ?? '').trim()
  return (WRITING_COMPUTE_TIERS as readonly string[]).includes(trimmed)
    ? (trimmed as WritingComputeTier)
    : DEFAULT_WRITING_COMPUTE_TIER
}

// tierRoleRows 返回指定档位下每个 role 的生效模型/思考描述，优先用后端导出的静态表，
// 回退到空（继承父级）以保证即使后端未提供也不崩。
export function tierRoleRows(
  tiers: WritingComputeTierRow[] | undefined,
  tier: string,
): Partial<Record<ComputeRole, WritingComputeTierRoleRow>> {
  const normalized = normalizeWritingComputeTier(tier)
  return tiers?.find((row) => row.id === normalized)?.roles ?? {}
}

// writingComputeTierFromSettings 读取有效设置中的档位，回退到默认档位。
export function writingComputeTierFromSettings(settings?: Settings): WritingComputeTier {
  return normalizeWritingComputeTier(settings?.writing_compute_tier)
}

// applyFastProfileToRows 把后端导出的档位表里"快速阶段"的 profile_id 替换为当前选中的
// 快速模型 id。后端 rows 只在 settings 保存后刷新，而选择器改动是即时的，此函数让汇总
// 展示与逐 SubAgent 的"继承"值立刻反映新选择，无需等待保存往返。判定"快速阶段"的依据是
// 后端已经把该 role 标为非继承（profile_id 非空），因此这里只替换非空项，不动继承项。
export function applyFastProfileToRows(
  rows: WritingComputeTierRow[],
  fastProfileID: string,
): WritingComputeTierRow[] {
  const id = (fastProfileID || '').trim() || DEFAULT_FAST_MODEL_PROFILE_ID
  return rows.map((row) => ({
    ...row,
    roles: Object.fromEntries(
      Object.entries(row.roles).map(([role, plan]) => [
        role,
        plan?.profile_id ? { ...plan, profile_id: id } : plan,
      ]),
    ),
  }))
}
