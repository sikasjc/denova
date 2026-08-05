import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeferredMarkdownEditor } from './DeferredMarkdownEditor'
import { LARGE_DOCUMENT_CHAR_THRESHOLD } from './editorDocument'

vi.mock('./MarkdownEditor', () => ({
  MarkdownEditor: ({ content }: { content: string }) => (
    <div data-testid="markdown-editor">{content.length}</div>
  ),
}))

const baseProps = {
  fileName: 'chapters/ch01.md',
  onSave: vi.fn(async () => true),
}

describe('DeferredMarkdownEditor', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('mounts the editor immediately for normal-sized documents', () => {
    render(<DeferredMarkdownEditor {...baseProps} content={'短章节内容'} />)

    expect(screen.getByTestId('markdown-editor')).toBeInTheDocument()
    expect(screen.queryByText('正在打开章节…')).not.toBeInTheDocument()
  })

  it('shows a loading indicator first, then mounts the editor for large documents', () => {
    const frames: FrameRequestCallback[] = []
    vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
      frames.push(callback)
      return frames.length
    }))
    vi.stubGlobal('cancelAnimationFrame', vi.fn())

    const largeContent = 'a'.repeat(LARGE_DOCUMENT_CHAR_THRESHOLD + 1)
    render(<DeferredMarkdownEditor {...baseProps} content={largeContent} />)

    // 首帧只有加载指示，编辑器尚未挂载。
    expect(screen.getByRole('status')).toHaveTextContent('正在打开章节…')
    expect(screen.queryByTestId('markdown-editor')).not.toBeInTheDocument()

    // 双 rAF 之后编辑器才挂载。
    act(() => { frames.shift()?.(0) })
    act(() => { frames.shift()?.(0) })

    expect(screen.getByTestId('markdown-editor')).toBeInTheDocument()
    expect(screen.queryByText('正在打开章节…')).not.toBeInTheDocument()
  })
})
