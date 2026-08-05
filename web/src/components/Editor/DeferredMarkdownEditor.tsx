import { useEffect, useState } from 'react'
import type { ComponentProps } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { MarkdownEditor } from './MarkdownEditor'
import { isLargeDocument } from './editorDocument'

type MarkdownEditorProps = ComponentProps<typeof MarkdownEditor>

/**
 * 超大章节挂载前的加载门：TipTap/ProseMirror 不做虚拟化，超大文档的一次性同步解析
 * 会阻塞主线程数秒。此包装器在真正挂载重编辑器前先绘制一个加载指示，把“像卡死的黑屏”
 * 变成明确的“正在打开章节…”，随后一帧再挂载编辑器（同步解析仍会占用该帧，但用户已看到加载态）。
 *
 * 普通大小文档直接挂载，行为与之前完全一致。切换文件由上层 `key` 触发整体重挂，
 * 因此每次打开新章节都会重新经过这道门。
 */
export function DeferredMarkdownEditor(props: MarkdownEditorProps) {
  const { t } = useTranslation()
  // 大文档首帧只渲染加载指示，让浏览器有机会绘制，再在下一帧挂载重编辑器。
  const [mountEditor, setMountEditor] = useState(() => !isLargeDocument(props.content))

  useEffect(() => {
    if (mountEditor) return
    // 双 rAF：第一帧提交加载指示并让浏览器绘制，第二帧再触发编辑器挂载。
    let secondFrame = 0
    const firstFrame = requestAnimationFrame(() => {
      secondFrame = requestAnimationFrame(() => setMountEditor(true))
    })
    return () => {
      cancelAnimationFrame(firstFrame)
      if (secondFrame) cancelAnimationFrame(secondFrame)
    }
  }, [mountEditor])

  if (!mountEditor) {
    return (
      <div
        className="flex flex-1 flex-col items-center justify-center gap-2 px-6 text-center text-sm text-[var(--nova-text-muted)]"
        role="status"
        aria-live="polite"
      >
        <div className="flex items-center gap-2">
          <Loader2 className="h-4 w-4 animate-spin" />
          <span>{t('editor.openingLargeChapter')}</span>
        </div>
        <span className="text-xs text-[var(--nova-text-faint)]">{t('editor.largeChapterHint')}</span>
      </div>
    )
  }

  return <MarkdownEditor {...props} />
}
