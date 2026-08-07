import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterAll, afterEach, beforeAll, vi } from 'vitest'
import { server } from './msw/server'

class MemoryStorage implements Storage {
  private values = new Map<string, string>()

  get length() {
    return this.values.size
  }

  clear() {
    this.values.clear()
  }

  getItem(key: string) {
    return this.values.get(key) ?? null
  }

  key(index: number) {
    return Array.from(this.values.keys())[index] ?? null
  }

  removeItem(key: string) {
    this.values.delete(key)
  }

  setItem(key: string, value: string) {
    this.values.set(key, String(value))
  }
}

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const testDOMRect = {
  width: 1,
  height: 1,
  top: 0,
  left: 0,
  right: 1,
  bottom: 1,
  x: 0,
  y: 0,
  toJSON: () => ({}),
} as DOMRect

// Install deterministic storage before importing i18n or persistence helpers.
// Recent Node releases expose an unusable experimental global localStorage
// unless the process receives --localstorage-file.
const localStorage = new MemoryStorage()
Object.defineProperty(window, 'localStorage', {
  configurable: true,
  value: localStorage,
})
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: localStorage,
})
localStorage.setItem('nova.locale.configured', 'zh-CN')
document.documentElement.lang = 'zh-CN'
const { setConfiguredLocale } = await import('@/i18n')
setConfiguredLocale('zh-CN')

Object.defineProperty(window, 'ResizeObserver', {
  writable: true,
  configurable: true,
  value: ResizeObserverMock,
})
Object.defineProperty(window.navigator, 'languages', {
  configurable: true,
  value: ['zh-CN'],
})
Object.defineProperty(window.navigator, 'language', {
  configurable: true,
  value: 'zh-CN',
})
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  configurable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
})
Object.defineProperty(HTMLElement.prototype, 'hasPointerCapture', {
  configurable: true,
  value: () => false,
})
Object.defineProperty(HTMLElement.prototype, 'setPointerCapture', {
  configurable: true,
  value: () => {},
})
Object.defineProperty(HTMLElement.prototype, 'releasePointerCapture', {
  configurable: true,
  value: () => {},
})
Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
  configurable: true,
  value: () => {},
})
Object.defineProperty(window, 'scrollBy', {
  configurable: true,
  value: () => {},
})
Object.defineProperty(window, 'scrollTo', {
  configurable: true,
  value: () => {},
})
Object.defineProperty(HTMLElement.prototype, 'scrollBy', {
  configurable: true,
  value: () => {},
})
Object.defineProperty(HTMLElement.prototype, 'scrollTo', {
  configurable: true,
  value: () => {},
})
Object.defineProperty(Element.prototype, 'getClientRects', {
  configurable: true,
  value: () => [testDOMRect],
})
Object.defineProperty(Range.prototype, 'getClientRects', {
  configurable: true,
  value: () => [testDOMRect],
})
Object.defineProperty(Range.prototype, 'getBoundingClientRect', {
  configurable: true,
  value: () => testDOMRect,
})
Object.defineProperty(document, 'elementFromPoint', {
  configurable: true,
  value: () => document.body,
})

beforeAll(() => {
  server.listen({ onUnhandledRequest: 'error' })
})

afterEach(() => {
  vi.useRealTimers()
  cleanup()
  server.resetHandlers()
})

afterAll(() => {
  server.close()
})
