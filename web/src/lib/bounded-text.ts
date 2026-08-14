export interface BoundedTextSlice {
  text: string
  truncated: boolean
}

export function hasNonWhitespace(value: string): boolean {
  for (const character of value) {
    if (character.trim().length > 0) return true
  }
  return false
}

/**
 * Returns at most `maxCodePoints` Unicode code points without materializing the
 * whole input as an array. This matters for streamed Agent output, where the
 * source can be much larger than the small preview the UI actually needs.
 */
export function takeCodePointPrefix(value: string, maxCodePoints: number): BoundedTextSlice {
  const limit = normalizeCodePointLimit(maxCodePoints)
  if (!value || limit === 0) return { text: '', truncated: value.length > 0 }
  if (value.length <= limit) return { text: value, truncated: false }

  let end = 0
  let count = 0
  for (const character of value) {
    if (count === limit) {
      return { text: value.slice(0, end), truncated: true }
    }
    end += character.length
    count += 1
  }
  return { text: value, truncated: false }
}

/**
 * Applies String.trim semantics while taking a prefix, without first creating
 * a near-source-sized trimmed string. It only scans past the requested prefix
 * when the remaining input might consist solely of removable trailing space.
 */
export function takeTrimmedCodePointPrefix(value: string, maxCodePoints: number): BoundedTextSlice {
  const limit = normalizeCodePointLimit(maxCodePoints)
  if (!value) return { text: '', truncated: false }

  let offset = 0
  let start = -1
  let prefixEnd = 0
  let lastNonWhitespaceEnd = 0
  let count = 0

  for (const character of value) {
    const characterStart = offset
    offset += character.length
    const whitespace = character.trim().length === 0
    if (start < 0) {
      if (whitespace) continue
      start = characterStart
    }

    count += 1
    if (count === limit) prefixEnd = offset
    if (whitespace) continue

    lastNonWhitespaceEnd = offset
    if (count > limit) {
      return {
        text: limit === 0 ? '' : value.slice(start, prefixEnd).trimEnd(),
        truncated: true,
      }
    }
  }

  if (start < 0) return { text: '', truncated: false }
  return { text: value.slice(start, lastNonWhitespaceEnd), truncated: false }
}

/**
 * Returns at most the last `maxCodePoints` Unicode code points with allocation
 * bounded by that limit rather than by the size of the source document.
 */
export function takeCodePointSuffix(value: string, maxCodePoints: number): BoundedTextSlice {
  const limit = normalizeCodePointLimit(maxCodePoints)
  if (!value || limit === 0) return { text: '', truncated: value.length > 0 }
  if (value.length <= limit) return { text: value, truncated: false }

  let start = value.length
  let count = 0
  while (start > 0 && count < limit) {
    start -= 1
    const lastUnit = value.charCodeAt(start)
    if (isLowSurrogate(lastUnit) && start > 0 && isHighSurrogate(value.charCodeAt(start - 1))) {
      start -= 1
    }
    count += 1
  }
  if (start === 0) return { text: value, truncated: false }
  return { text: value.slice(start), truncated: true }
}

function normalizeCodePointLimit(value: number) {
  if (!Number.isFinite(value) || value <= 0) return 0
  return Math.floor(value)
}

function isHighSurrogate(value: number) {
  return value >= 0xd800 && value <= 0xdbff
}

function isLowSurrogate(value: number) {
  return value >= 0xdc00 && value <= 0xdfff
}
