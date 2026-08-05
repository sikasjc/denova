import type { TFunction } from 'i18next'
import type { WritingQuickActionSettings } from '@/features/settings/types'

const TARGET_PLACEHOLDER = '{target}'

const DEFAULT_ACTIONS = [
  { id: 'next-group', promptKey: 'chat.quick.prompt.nextGroup' },
  { id: 'write-next-chapter', promptKey: 'chat.quick.prompt.writeNextChapter' },
  { id: 'continue-paragraph', promptKey: 'chat.quick.prompt.continueParagraph' },
  { id: 'polish-chapter', promptKey: 'chat.quick.prompt.polishChapter' },
  { id: 'sync-state', promptKey: 'chat.quick.prompt.finalizeState' },
  { id: 'consistency-check', promptKey: 'chat.quick.prompt.consistencyCheck' },
]

const DEFAULT_LABEL_KEYS: Record<string, string> = {
  'next-group': 'chat.quick.nextGroup',
  'write-next-chapter': 'chat.quick.writeNextChapter',
  'continue-paragraph': 'chat.quick.continueParagraph',
  'polish-chapter': 'chat.quick.polishChapter',
  'sync-state': 'chat.quick.finalizeState',
  'consistency-check': 'chat.quick.consistencyCheck',
}

export function resolveWritingQuickActions(actions: WritingQuickActionSettings[] | undefined, t: TFunction) {
  return actions === undefined ? defaultWritingQuickActions(t) : actions
}

export function writingQuickActionLabel(action: WritingQuickActionSettings, t: TFunction) {
  const customLabel = action.label?.trim()
  if (customLabel) return customLabel
  const labelKey = DEFAULT_LABEL_KEYS[action.id]
  return labelKey ? t(labelKey) : t('settings.quickActions.untitled')
}

export function interpolateWritingQuickActionPrompt(prompt: string, target: string) {
  return prompt.replaceAll(TARGET_PLACEHOLDER, target)
}

export function writingQuickActionTarget(chapterTitle: string | undefined, selectedFile: string | null, t: TFunction) {
  if (chapterTitle) return t('chat.quick.targetChapter', { title: chapterTitle })
  if (selectedFile) return t('chat.quick.targetFile', { file: selectedFile })
  return t('chat.quick.targetWork')
}

export function resolveWritingQuickActionItems(actions: WritingQuickActionSettings[], target: string, t: TFunction) {
  return actions.map((action) => ({
    ...action,
    label: writingQuickActionLabel(action, t),
    prompt: interpolateWritingQuickActionPrompt(action.prompt, target),
  }))
}

export function defaultWritingQuickActions(t: TFunction): WritingQuickActionSettings[] {
  return DEFAULT_ACTIONS.map((action) => ({
    id: action.id,
    prompt: t(action.promptKey),
  }))
}
