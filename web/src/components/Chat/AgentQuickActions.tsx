import { FileText, PenLine, SearchCheck, Settings2, Sparkles, WandSparkles, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { ChapterSummary } from '@/lib/api'
import type { WritingQuickActionSettings } from '@/features/settings/types'
import { resolveWritingQuickActionItems, writingQuickActionTarget } from '@/features/writing-quick-actions/quick-actions'

const ACTION_ICONS: Record<string, LucideIcon> = {
  'next-group': FileText,
  'write-next-chapter': PenLine,
  'continue-paragraph': PenLine,
  'polish-chapter': WandSparkles,
  'sync-state': FileText,
  'consistency-check': SearchCheck,
}

export function AgentQuickActions({
  actions,
  chapter,
  selectedFile,
  onPrefill,
  onOpenSettings,
}: {
  actions: WritingQuickActionSettings[]
  chapter?: ChapterSummary
  selectedFile: string | null
  onPrefill: (message: string) => void
  onOpenSettings: () => void
}) {
  const { t } = useTranslation()
  const target = writingQuickActionTarget(chapter?.display_title, selectedFile, t)
  const resolvedActions = resolveWritingQuickActionItems(actions, target, t)

  return (
    <div className="border-b border-[var(--nova-border)] bg-[var(--nova-bg)] p-3">
      <div className="mb-2 flex items-center gap-2 text-xs font-medium text-[var(--nova-text-muted)]">
        <Sparkles className="h-3.5 w-3.5 text-[var(--nova-text-muted)]" />
        <span className="min-w-0 flex-1">{t('chat.quickActions')}</span>
        <button
          type="button"
          className="nova-nav-item rounded p-1 text-[var(--nova-text-faint)] hover:text-[var(--nova-text)]"
          onClick={onOpenSettings}
          aria-label={t('chat.quick.customize')}
          title={t('chat.quick.customize')}
        >
          <Settings2 className="h-3.5 w-3.5" />
        </button>
      </div>
      {resolvedActions.length > 0 ? (
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          {resolvedActions.map((action) => {
            const Icon = ACTION_ICONS[action.id] ?? Sparkles
            return (
              <button
                key={action.id}
                type="button"
                className="nova-nav-item flex min-w-0 items-center gap-2 border border-[var(--nova-border)] bg-[var(--nova-surface)] px-3 py-2 text-left text-xs"
                onClick={() => onPrefill(action.prompt)}
                disabled={!action.prompt.trim()}
                title={action.label}
              >
                <Icon className="h-3.5 w-3.5 shrink-0 text-[var(--nova-text-muted)]" />
                <span className="truncate">{action.label}</span>
              </button>
            )
          })}
        </div>
      ) : (
        <button
          type="button"
          className="w-full rounded-[var(--nova-radius)] border border-dashed border-[var(--nova-border)] bg-[var(--nova-surface)] px-3 py-3 text-left text-xs text-[var(--nova-text-faint)] hover:text-[var(--nova-text-muted)]"
          onClick={onOpenSettings}
        >
          {t('chat.quick.empty')}
        </button>
      )}
      <div className="mt-2 text-[11px] leading-4 text-[var(--nova-text-faint)]">
        {t('chat.quick.prefillHint')}
      </div>
    </div>
  )
}
