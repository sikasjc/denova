import { describe, expect, it } from 'vitest'
import { splitMarkdownBlocks } from './markdown-blocks'

describe('splitMarkdownBlocks', () => {
  it('空输入返回空数组', () => {
    expect(splitMarkdownBlocks('')).toEqual([])
  })

  it('按空行切分为顶层块', () => {
    const content = '# 标题\n\n第一段。\n\n- 条目 A\n- 条目 B\n\n> 引用'
    expect(splitMarkdownBlocks(content)).toEqual([
      '# 标题',
      '第一段。',
      '- 条目 A\n- 条目 B',
      '> 引用',
    ])
  })

  it('合并连续空行，不产生空块', () => {
    expect(splitMarkdownBlocks('段一\n\n\n\n段二')).toEqual(['段一', '段二'])
  })

  it('不在围栏代码块内部切分（代码块含空行）', () => {
    const content = '前言\n\n```js\nconst a = 1\n\nconst b = 2\n```\n\n结尾'
    expect(splitMarkdownBlocks(content)).toEqual([
      '前言',
      '```js\nconst a = 1\n\nconst b = 2\n```',
      '结尾',
    ])
  })

  it('流式中途未闭合的围栏视为单块，直到闭合', () => {
    const content = '前言\n\n```js\nconst a = 1\n\nconst b = 2'
    expect(splitMarkdownBlocks(content)).toEqual([
      '前言',
      '```js\nconst a = 1\n\nconst b = 2',
    ])
  })

  it('~~~ 围栏与 ``` 各自独立配对', () => {
    const content = '~~~\ncode\n\nmore\n~~~'
    expect(splitMarkdownBlocks(content)).toEqual(['~~~\ncode\n\nmore\n~~~'])
  })

  it('已封口块内容稳定，仅末尾块随流式增长（值相等以支持记忆化）', () => {
    const first = splitMarkdownBlocks('# 标题\n\n正在输入')
    const second = splitMarkdownBlocks('# 标题\n\n正在输入的更多内容')
    expect(first[0]).toBe(second[0])
    expect(first[1]).not.toBe(second[1])
  })
})
