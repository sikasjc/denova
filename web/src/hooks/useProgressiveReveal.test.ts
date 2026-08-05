import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useProgressiveReveal } from './useProgressiveReveal'

describe('useProgressiveReveal', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function stubRaf() {
    const frames: FrameRequestCallback[] = []
    vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
      frames.push(callback)
      return frames.length
    }))
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
    return {
      runNextFrame: () => act(() => { frames.shift()?.(0) }),
      pending: () => frames.length,
    }
  }

  it('shows all items at once when disabled', () => {
    const raf = stubRaf()
    const { result } = renderHook(() => useProgressiveReveal(500, { enabled: false, initialBatch: 10, step: 5 }))

    expect(result.current).toBe(500)
    expect(raf.pending()).toBe(0)
  })

  it('reveals the initial batch immediately and grows one step per frame', () => {
    const raf = stubRaf()
    const { result } = renderHook(() => useProgressiveReveal(30, { enabled: true, initialBatch: 10, step: 8 }))

    expect(result.current).toBe(10)
    raf.runNextFrame()
    expect(result.current).toBe(18)
    raf.runNextFrame()
    expect(result.current).toBe(26)
    raf.runNextFrame()
    expect(result.current).toBe(30)
  })

  it('never schedules a frame when everything already fits in the initial batch', () => {
    const raf = stubRaf()
    const { result } = renderHook(() => useProgressiveReveal(6, { enabled: true, initialBatch: 10, step: 5 }))

    expect(result.current).toBe(6)
    expect(raf.pending()).toBe(0)
  })

  it('resets to the initial batch when the total changes', () => {
    const raf = stubRaf()
    const { result, rerender } = renderHook(
      ({ total }) => useProgressiveReveal(total, { enabled: true, initialBatch: 10, step: 8 }),
      { initialProps: { total: 30 } },
    )

    raf.runNextFrame()
    expect(result.current).toBe(18)

    rerender({ total: 50 })
    expect(result.current).toBe(10)
  })
})
