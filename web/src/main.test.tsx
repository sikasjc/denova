import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PropsWithChildren } from 'react'

const startupMocks = vi.hoisted(() => ({
  fetchSettings: vi.fn(),
  installGlobalRuntimeLoggers: vi.fn(),
  recordRuntimeLog: vi.fn(),
  render: vi.fn(),
  scheduleWhiteScreenCheck: vi.fn(),
}))

vi.mock('react-dom/client', () => ({
  createRoot: () => ({ render: startupMocks.render }),
}))

vi.mock('@tanstack/react-query', () => ({
  QueryClientProvider: ({ children }: PropsWithChildren) => children,
}))

vi.mock('next-themes', () => ({
  ThemeProvider: ({ children }: PropsWithChildren) => children,
}))

vi.mock('@/features/settings/api', () => ({
  fetchSettings: startupMocks.fetchSettings,
}))

vi.mock('@/features/settings/font-variables', () => ({
  applyFontSettings: vi.fn(),
  fontSettingsFromEffective: vi.fn(),
}))

vi.mock('@/i18n', () => ({
  setConfiguredLocale: vi.fn(),
}))

vi.mock('@/lib/runtimeLog', () => ({
  installGlobalRuntimeLoggers: startupMocks.installGlobalRuntimeLoggers,
  recordRuntimeLog: startupMocks.recordRuntimeLog,
  scheduleWhiteScreenCheck: startupMocks.scheduleWhiteScreenCheck,
}))

vi.mock('@/lib/query-client', () => ({
  queryClient: {},
}))

vi.mock('@/components/RuntimeErrorBoundary', () => ({
  RuntimeErrorBoundary: ({ children }: PropsWithChildren) => children,
}))

vi.mock('@/components/ui/sonner', () => ({
  Toaster: () => null,
}))

vi.mock('@/components/ui/tooltip', () => ({
  TooltipProvider: ({ children }: PropsWithChildren) => children,
}))

vi.mock('./App', () => ({ default: () => null }))

describe('application startup', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.clearAllMocks()
    document.body.innerHTML = '<div id="root"></div>'
  })

  it('mounts the application shell while remote settings are still pending', async () => {
    startupMocks.fetchSettings.mockReturnValue(new Promise(() => {}))

    await import('./main')

    expect(startupMocks.render).toHaveBeenCalledTimes(1)
    expect(startupMocks.scheduleWhiteScreenCheck).toHaveBeenCalledWith(document.getElementById('root'))
  })
})
