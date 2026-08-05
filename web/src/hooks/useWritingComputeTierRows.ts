import { useEffect, useState } from 'react'
import { fetchSettings } from '@/features/settings/api'
import type { WritingComputeTierRow } from '@/features/settings/types'

// useWritingComputeTierRows 拉取后端导出的写作算力档位 × role 静态映射表，
// 供编辑框档位选择器与 Agents 页汇总展示"每档各阶段用什么模型/思考"。
// 映射表本身是稳定的（随后端版本变化），因此仅在挂载和 settings 更新事件时刷新。
export function useWritingComputeTierRows(): WritingComputeTierRow[] {
  const [rows, setRows] = useState<WritingComputeTierRow[]>([])

  useEffect(() => {
    let cancelled = false
    const load = () => {
      fetchSettings()
        .then((settings) => {
          if (!cancelled) setRows(settings.writing_compute_tiers ?? [])
        })
        .catch((error) => {
          console.warn('[compute-tier] load tier rows failed', { error })
          if (!cancelled) setRows([])
        })
    }
    load()
    window.addEventListener('nova:settings-updated', load)
    return () => {
      cancelled = true
      window.removeEventListener('nova:settings-updated', load)
    }
  }, [])

  return rows
}
