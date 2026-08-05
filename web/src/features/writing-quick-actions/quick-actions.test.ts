import { describe, expect, it } from 'vitest'
import type { TFunction } from 'i18next'
import {
  interpolateWritingQuickActionPrompt,
  resolveWritingQuickActions,
  resolveWritingQuickActionItems,
  writingQuickActionTarget,
  writingQuickActionLabel,
} from './quick-actions'

const translations: Record<string, string> = {
  'chat.quick.nextGroup': 'Next Group Outline',
  'chat.quick.prompt.nextGroup': 'Plan the next group.',
  'chat.quick.prompt.writeNextChapter': 'Write the next chapter.',
  'chat.quick.prompt.continueParagraph': 'Continue {target}.',
  'chat.quick.prompt.polishChapter': 'Polish {target}.',
  'chat.quick.prompt.finalizeState': 'Sync {target}.',
  'chat.quick.prompt.consistencyCheck': 'Check {target}.',
  'settings.quickActions.untitled': 'Untitled Action',
}
const t = ((key: string) => translations[key] || key) as TFunction

describe('Writing quick actions', () => {
  it('uses localized built-in actions only when no override exists', () => {
    const defaults = resolveWritingQuickActions(undefined, t)

    expect(defaults).toHaveLength(6)
    expect(defaults[0]).toEqual({ id: 'next-group', prompt: 'Plan the next group.' })
    expect(writingQuickActionLabel(defaults[0], t)).toBe('Next Group Outline')
    expect(resolveWritingQuickActions([], t)).toEqual([])
  })

  it('replaces every target placeholder without changing custom text', () => {
    expect(interpolateWritingQuickActionPrompt('Review {target}, then compare {target}.', 'chapter 3'))
      .toBe('Review chapter 3, then compare chapter 3.')
  })

  it('resolves the active file target for both empty-state and persistent menus', () => {
    const action = { id: 'custom', label: 'Review', prompt: 'Review {target}.' }
    const target = writingQuickActionTarget(undefined, 'chapters/ch03.md', ((key: string, options?: Record<string, unknown>) => (
      key === 'chat.quick.targetFile' ? `file ${options?.file}` : key
    )) as TFunction)

    expect(resolveWritingQuickActionItems([action], target, t)).toEqual([
      { id: 'custom', label: 'Review', prompt: 'Review file chapters/ch03.md.' },
    ])
  })
})
