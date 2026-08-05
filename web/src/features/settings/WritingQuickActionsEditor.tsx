import { ArrowDown, ArrowUp, Plus, RotateCcw, Trash2 } from 'lucide-react'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import type { WritingQuickActionSettings } from './types'
import {
  resolveWritingQuickActions,
  writingQuickActionLabel,
} from '@/features/writing-quick-actions/quick-actions'

const MAX_WRITING_QUICK_ACTIONS = 24
const MAX_QUICK_ACTION_LABEL_LENGTH = 80
const MAX_QUICK_ACTION_PROMPT_LENGTH = 262144

export function WritingQuickActionsEditor({
  actions,
  effectiveActions,
  hasOverride,
  onChange,
}: {
  actions: WritingQuickActionSettings[] | undefined
  effectiveActions: WritingQuickActionSettings[] | undefined
  hasOverride: boolean
  onChange: (actions: WritingQuickActionSettings[] | undefined) => void
}) {
  const { t } = useTranslation()
  const nextIDRef = useRef(1)
  const visibleActions = hasOverride
    ? (actions ?? resolveWritingQuickActions(undefined, t))
    : resolveWritingQuickActions(effectiveActions, t)
  const addAction = () => {
    const usedIDs = new Set(visibleActions.map((action) => action.id))
    let id = ''
    do {
      id = `custom-${Date.now()}-${nextIDRef.current}`
      nextIDRef.current += 1
    } while (usedIDs.has(id))
    onChange([...visibleActions, { id, label: '', prompt: '' }])
  }
  const updateAction = (index: number, patch: Partial<WritingQuickActionSettings>) => {
    onChange(visibleActions.map((action, actionIndex) => (
      actionIndex === index ? { ...action, ...patch } : action
    )))
  }
  const removeAction = (index: number) => {
    onChange(visibleActions.filter((_, actionIndex) => actionIndex !== index))
  }
  const moveAction = (index: number, direction: -1 | 1) => {
    const targetIndex = index + direction
    if (targetIndex < 0 || targetIndex >= visibleActions.length) return
    const next = [...visibleActions]
    const [action] = next.splice(index, 1)
    next.splice(targetIndex, 0, action)
    onChange(next)
  }

  return (
    <div className="nova-settings-row rounded-md px-2 py-1.5">
      <div className="mb-2 flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="font-medium text-[var(--nova-text)]">{t('settings.quickActions.title')}</div>
          <div className="mt-0.5 text-[11px] leading-4 text-[var(--nova-text-faint)]">
            {t('settings.quickActions.description')}
          </div>
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onChange(undefined)}
          >
            <RotateCcw data-icon="inline-start" />
            {t('settings.quickActions.restoreDefaults')}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={visibleActions.length >= MAX_WRITING_QUICK_ACTIONS}
            onClick={addAction}
          >
            <Plus data-icon="inline-start" />
            {t('settings.quickActions.add')}
          </Button>
        </div>
      </div>
      <div className="mb-2 rounded-[var(--nova-radius)] border border-[var(--nova-border)] bg-[var(--nova-surface-2)] px-2.5 py-2 text-[11px] leading-4 text-[var(--nova-text-faint)]">
        {t('settings.quickActions.targetHelp')}
      </div>
      <div className="flex flex-col gap-2">
        {visibleActions.length === 0 ? (
          <div className="rounded-[var(--nova-radius)] border border-dashed border-[var(--nova-border)] bg-[var(--nova-surface-2)] px-3 py-4 text-center text-[var(--nova-text-faint)]">
            {t('settings.quickActions.empty')}
          </div>
        ) : null}
        {visibleActions.map((action, index) => (
          <div
            key={action.id}
            className="rounded-[var(--nova-radius)] border border-[var(--nova-border)] bg-[var(--nova-surface-2)] p-2.5"
          >
            <div className="mb-2 flex items-center gap-2">
              <span className="min-w-0 flex-1 truncate font-medium text-[var(--nova-text)]">
                {action.label?.trim() || writingQuickActionLabel(action, t) || t('settings.quickActions.untitled')}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                disabled={index === 0}
                onClick={() => moveAction(index, -1)}
                aria-label={t('settings.quickActions.moveUp')}
                title={t('settings.quickActions.moveUp')}
              >
                <ArrowUp />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                disabled={index === visibleActions.length - 1}
                onClick={() => moveAction(index, 1)}
                aria-label={t('settings.quickActions.moveDown')}
                title={t('settings.quickActions.moveDown')}
              >
                <ArrowDown />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => removeAction(index)}
                aria-label={t('settings.quickActions.delete')}
                title={t('settings.quickActions.delete')}
              >
                <Trash2 />
              </Button>
            </div>
            <div className="grid gap-2">
              <label className="grid gap-1">
                <span className="text-[11px] text-[var(--nova-text-muted)]">{t('settings.quickActions.label')}</span>
                <Input
                  value={action.label ?? ''}
                  maxLength={MAX_QUICK_ACTION_LABEL_LENGTH}
                  placeholder={writingQuickActionLabel(action, t)}
                  onChange={(event) => updateAction(index, { label: event.target.value })}
                />
              </label>
              <label className="grid gap-1">
                <span className="text-[11px] text-[var(--nova-text-muted)]">{t('settings.quickActions.prompt')}</span>
                <Textarea
                  value={action.prompt}
                  maxLength={MAX_QUICK_ACTION_PROMPT_LENGTH}
                  rows={5}
                  className="min-h-24 resize-y text-xs leading-5"
                  placeholder={t('settings.quickActions.promptPlaceholder')}
                  onChange={(event) => updateAction(index, { prompt: event.target.value })}
                />
              </label>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
