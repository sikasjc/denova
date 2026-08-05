import { Settings2, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { WritingQuickActionSettings } from '@/features/settings/types'
import { resolveWritingQuickActionItems, writingQuickActionTarget } from '@/features/writing-quick-actions/quick-actions'

export function WritingQuickActionsMenu({
  actions,
  chapterTitle,
  selectedFile,
  disabled,
  onPrefill,
  onOpenSettings,
}: {
  actions: WritingQuickActionSettings[]
  chapterTitle?: string
  selectedFile: string | null
  disabled: boolean
  onPrefill: (prompt: string) => void
  onOpenSettings: () => void
}) {
  const { t } = useTranslation()
  const target = writingQuickActionTarget(chapterTitle, selectedFile, t)
  const resolvedActions = resolveWritingQuickActionItems(actions, target, t)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          size="icon-sm"
          className="nova-agent-composer-icon h-8 w-8 shrink-0 rounded-[10px] border border-[var(--nova-border)] bg-[var(--nova-surface)] text-[var(--nova-text-muted)] hover:bg-[var(--nova-hover)] hover:text-[var(--nova-text)] disabled:opacity-45"
          disabled={disabled}
          aria-label={t('chat.quick.open')}
          title={t('chat.quick.open')}
        >
          <Sparkles className="h-3.5 w-3.5" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        side="top"
        className="w-80 border-[var(--nova-border)] bg-[var(--nova-surface-2)] p-2 text-[var(--nova-text)]"
      >
        <div className="px-2 pb-2 pt-1">
          <div className="text-xs font-medium text-[var(--nova-text)]">{t('chat.quickActions')}</div>
          <div className="mt-0.5 text-[11px] leading-4 text-[var(--nova-text-faint)]">{t('chat.quick.currentSessionHint')}</div>
        </div>
        <DropdownMenuSeparator className="bg-[var(--nova-border-soft)]" />
        {resolvedActions.length > 0 ? resolvedActions.map((action) => (
          <DropdownMenuItem
            key={action.id}
            disabled={!action.prompt.trim()}
            onSelect={() => onPrefill(action.prompt)}
            className="cursor-pointer text-xs focus:bg-[var(--nova-active)] focus:text-[var(--nova-text)]"
          >
            <Sparkles className="h-3.5 w-3.5 text-[var(--nova-text-faint)]" />
            <span className="min-w-0 flex-1 truncate">{action.label}</span>
          </DropdownMenuItem>
        )) : (
          <div className="px-2 py-3 text-xs leading-5 text-[var(--nova-text-faint)]">{t('chat.quick.empty')}</div>
        )}
        <DropdownMenuSeparator className="bg-[var(--nova-border-soft)]" />
        <DropdownMenuItem
          onSelect={onOpenSettings}
          className="cursor-pointer text-xs focus:bg-[var(--nova-active)] focus:text-[var(--nova-text)]"
        >
          <Settings2 className="h-3.5 w-3.5" />
          {t('chat.quick.customize')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
