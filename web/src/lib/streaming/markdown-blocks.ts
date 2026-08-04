/**
 * 把 Markdown 文本按顶层块边界（空行分隔）切分，用于流式增量渲染。
 *
 * 设计要点：Markdown 的块级边界恰好就是空行，因此"逐块独立渲染"与"整段一次渲染"
 * 产出的顶层元素序列一致。流式时只有末尾未封口的块在增长，已封口的块内容稳定、
 * 可被记忆化跳过重解析，从而把整段每帧重解析的 O(n^2) 压成 O(n)。
 *
 * 关键约束：不得在 ``` / ~~~ 围栏代码块内部切分（代码块允许包含空行）。
 * 流式中途尚未闭合的围栏，会把从围栏起始到当前末尾的内容作为同一个块，
 * 直到围栏闭合，保证渲染结果与最终一致。
 */

const FENCE_PATTERN = /^(\s*)(`{3,}|~{3,})/

/** 将 Markdown 切分为可独立渲染的顶层块；空输入返回空数组。 */
export function splitMarkdownBlocks(content: string): string[] {
  if (!content) return []
  const lines = content.split('\n')
  const blocks: string[] = []
  let current: string[] = []
  // 当前是否位于围栏代码块内部；fenceMarker 记录起始围栏字符（` 或 ~），仅同字符可闭合。
  let inFence = false
  let fenceMarker = ''

  const flush = () => {
    if (current.length === 0) return
    blocks.push(current.join('\n'))
    current = []
  }

  for (const line of lines) {
    const fenceMatch = FENCE_PATTERN.exec(line)
    if (fenceMatch) {
      const marker = fenceMatch[2][0]
      if (!inFence) {
        inFence = true
        fenceMarker = marker
      } else if (marker === fenceMarker) {
        inFence = false
        fenceMarker = ''
      }
      current.push(line)
      continue
    }

    // 围栏内的空行属于代码块，不作为块边界。
    if (!inFence && line.trim() === '') {
      flush()
      continue
    }

    current.push(line)
  }

  flush()
  return blocks
}
