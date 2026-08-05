import { useEffect, useState } from 'react'

export interface ProgressiveRevealOptions {
  /** 关闭时直接一次性全部可见（如流式态、或内容量低于阈值）。 */
  enabled: boolean
  /** 首屏立即挂载的数量，用于填满一屏、避免出现空白。 */
  initialBatch: number
  /** 后续每个动画帧追加挂载的数量。 */
  step: number
}

/**
 * 分帧渐进揭示：把大量同类子项的挂载拆到多个动画帧完成，避免一次 commit 同步
 * 挂载全部子项造成主线程长任务卡死。返回当前应渲染的子项数量。
 *
 * 关键约束：
 * - `enabled` 为 false 时立即返回 `total`（不分帧），保证流式与小内容路径行为不变。
 * - `total` 变化（内容替换/切换消息）时从首屏批次重新开始，避免揭示状态错位。
 * - 使用 `requestAnimationFrame` 而非定时器，让浏览器在两帧之间处理布局与滚动。
 */
export function useProgressiveReveal(total: number, { enabled, initialBatch, step }: ProgressiveRevealOptions): number {
  const initialVisible = enabled ? Math.min(total, initialBatch) : total
  const [visible, setVisible] = useState(initialVisible)

  useEffect(() => {
    if (!enabled) {
      setVisible(total)
      return
    }
    // 内容变化：先重置到首屏批次，再逐帧追加，直到全部可见。
    let current = Math.min(total, initialBatch)
    setVisible(current)
    if (current >= total) return

    let frameID = requestAnimationFrame(function reveal() {
      current = Math.min(total, current + step)
      setVisible(current)
      if (current < total) {
        frameID = requestAnimationFrame(reveal)
      }
    })
    return () => cancelAnimationFrame(frameID)
  }, [total, enabled, initialBatch, step])

  // 未启用时忽略内部 state，始终全部可见（同一 hook 顺序，避免条件调用）。
  return enabled ? Math.min(visible, total) : total
}
