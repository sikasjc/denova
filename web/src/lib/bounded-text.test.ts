import { describe, expect, it, vi } from 'vitest'
import { takeCodePointPrefix, takeCodePointSuffix, takeTrimmedCodePointPrefix } from './bounded-text'

describe('bounded Unicode text slices', () => {
  it('keeps surrogate pairs intact at prefix and suffix boundaries', () => {
    expect(takeCodePointPrefix('甲😀乙', 2)).toEqual({ text: '甲😀', truncated: true })
    expect(takeCodePointSuffix('甲😀乙', 2)).toEqual({ text: '😀乙', truncated: true })
  })

  it('does not expand a large source into an array for a short preview', () => {
    const arrayFrom = vi.spyOn(Array, 'from')
    const source = '审'.repeat(2_000_000)

    try {
      const result = takeTrimmedCodePointPrefix(source, 220)

      expect(result).toEqual({ text: '审'.repeat(220), truncated: true })
      expect(arrayFrom).not.toHaveBeenCalled()
    } finally {
      arrayFrom.mockRestore()
    }
  })

  it('trims surrounding whitespace without counting it against the prefix', () => {
    expect(takeTrimmedCodePointPrefix(' \n甲😀乙 \t', 2)).toEqual({ text: '甲😀', truncated: true })
    expect(takeTrimmedCodePointPrefix(' \n甲😀 \t', 2)).toEqual({ text: '甲😀', truncated: false })
    expect(takeTrimmedCodePointPrefix(' \n\t', 2)).toEqual({ text: '', truncated: false })
  })

  it('reports exact and zero-length bounds without changing the source', () => {
    expect(takeCodePointPrefix('完整文本', 4)).toEqual({ text: '完整文本', truncated: false })
    expect(takeCodePointSuffix('完整文本', 4)).toEqual({ text: '完整文本', truncated: false })
    expect(takeCodePointPrefix('内容', 0)).toEqual({ text: '', truncated: true })
  })
})
