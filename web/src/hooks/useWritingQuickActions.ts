import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { fetchSettings } from '@/features/settings/api'
import type { WritingQuickActionSettings } from '@/features/settings/types'
import { resolveWritingQuickActions } from '@/features/writing-quick-actions/quick-actions'

export function useWritingQuickActions(workspace: string) {
  const { t } = useTranslation()
  const [configuredActions, setConfiguredActions] = useState<WritingQuickActionSettings[] | undefined>(undefined)

  const load = useCallback(() => {
    if (!workspace) {
      setConfiguredActions(undefined)
      return
    }
    fetchSettings()
      .then((settings) => setConfiguredActions(settings.effective.writing_quick_actions))
      .catch((error) => {
        console.warn('[useWritingQuickActions.ts] failed to load Writing quick actions; using defaults', { error })
        setConfiguredActions(undefined)
      })
  }, [workspace])

  useEffect(() => {
    load()
    window.addEventListener('nova:settings-updated', load)
    return () => window.removeEventListener('nova:settings-updated', load)
  }, [load])

  return resolveWritingQuickActions(configuredActions, t)
}
