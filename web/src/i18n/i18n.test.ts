import { afterEach, describe, expect, it, vi } from 'vitest'
import zhCN from './locales/zh-CN'
import enUS from './locales/en-US'

describe('i18n', () => {
  afterEach(() => {
    window.localStorage.clear()
    setBrowserLanguage('zh-CN')
  })

  it('resolves auto from browser language', async () => {
    const { resolveLocale } = await import('./index')

    expect(resolveLocale('auto', 'zh-Hans')).toBe('zh-CN')
    expect(resolveLocale('auto', 'en-GB')).toBe('en-US')
  })

  it('keeps locale resource keys aligned', () => {
    expect(Object.keys(enUS).sort()).toEqual(Object.keys(zhCN).sort())
  })

  it('contains writing-agent init copy in both locales', () => {
    const requiredKeys = [
      'loreInit.ideTitle',
      'loreInit.ideDescription',
      'loreInit.ideAction',
      'writingAgent.initPrompt',
    ]

    for (const key of requiredKeys) {
      expect((zhCN as Record<string, string>)[key]).toBeTruthy()
      expect((enUS as Record<string, string>)[key]).toBeTruthy()
    }
  })

  it('keeps next-chapter prompts aligned with Writing Skill review stages', () => {
    expect(zhCN['chat.quick.prompt.writeNextChapter']).toContain('严格按当前 Writing Skill 完成审稿、修订和最终机械验证')
    expect(zhCN['chat.quick.prompt.writeNextChapter']).toContain('不要额外增加父 Agent 自审')
    expect(enUS['chat.quick.prompt.writeNextChapter']).toContain('Follow the active Writing Skill through review, revision, and final mechanical verification')
    expect(enUS['chat.quick.prompt.writeNextChapter']).toContain('without adding a parent-Agent self-review stage')
  })

  it('boots from the locally cached configured locale before browser language', async () => {
    vi.resetModules()
    setBrowserLanguage('en-US')
    window.localStorage.setItem('nova.locale.configured', 'zh-CN')

    const { default: i18next, getConfiguredLocale, getResolvedLocale } = await import('./index')

    expect(getConfiguredLocale()).toBe('zh-CN')
    expect(getResolvedLocale()).toBe('zh-CN')
    expect(i18next.language).toBe('zh-CN')
    expect(document.documentElement.lang).toBe('zh-CN')
  })

  it('persists the configured locale after settings are loaded', async () => {
    vi.resetModules()
    const { setConfiguredLocale } = await import('./index')

    setConfiguredLocale('en-US')

    expect(window.localStorage.getItem('nova.locale.configured')).toBe('en-US')
  })
})

function setBrowserLanguage(language: string) {
  Object.defineProperty(window.navigator, 'languages', {
    configurable: true,
    value: [language],
  })
  Object.defineProperty(window.navigator, 'language', {
    configurable: true,
    value: language,
  })
}
