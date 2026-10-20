// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { lastMessageText, messagePreviewText } from './messagePreview'

describe('lastMessageText', () => {
  it('selects the last nonempty message block without cutting its paragraphs', () => {
    const final = '已确认是**修缺陷**。\n\n请补充现象与期望。'
    expect(lastMessageText([
      { kind: 'message', text: '先检查已有产物。' },
      { kind: 'tool' },
      { kind: 'message', text: final },
      { kind: 'thought', text: '内部过程' },
      { kind: 'message', text: '  \n' },
    ], '拼接的全部正文')).toBe(final)
  })

  it('falls back to transcript text for old replies without message blocks', () => {
    expect(lastMessageText(undefined, '旧回复')).toBe('旧回复')
    expect(lastMessageText([], '旧回复')).toBe('旧回复')
    expect(lastMessageText([{ kind: 'tool' }])).toBe('')
  })
})

describe('messagePreviewText', () => {
  it('extracts the reported bold text and Markdown blocks as a readable summary', () => {
    expect(messagePreviewText('已记下：本次是**修缺陷**。\n\n## 下一步\n\n- 选择模块\n- 补充实际表现'))
      .toBe('已记下：本次是修缺陷。 下一步 选择模块 补充实际表现')
  })

  it('preserves filenames, code and literal punctuation instead of deleting characters', () => {
    expect(messagePreviewText('查看 `feedback_index.json`，计算 2 * 3；\\*原样\\*。\n\n```ts\nconst file_name = "**literal**"\n```'))
      .toBe('查看 feedback_index.json，计算 2 * 3；*原样*。 const file_name = "**literal**"')
  })

  it('keeps link labels and image descriptions without destinations or image UI', () => {
    expect(messagePreviewText('[检查文档][d]，![验收截图](https://example.com/image.png)\n\n[d]: https://example.com/check'))
      .toBe('检查文档，验收截图')
  })

  it('separates adjacent paragraphs, breaks and table cells and decodes entities', () => {
    expect(messagePreviewText('甲 &amp; 乙\n下一行\n\n| 检查项 | 结果 |\n| --- | --- |\n| 代码 | 通过 |'))
      .toBe('甲 & 乙 下一行 检查项 结果 代码 通过')
  })

  it('keeps HTML examples as literal text in an inert fragment', () => {
    const source = '<img src=x onerror=alert(1)>示例<script>代码</script> [危险链接](javascript:alert(1))'
    expect(messagePreviewText(source)).toContain('<img src=x onerror=alert(1)>示例<script>代码</script>')
    expect(messagePreviewText(source)).toContain('危险链接')
    expect(document.querySelector('img, script, a')).toBeNull()
  })

  it('handles empty messages', () => {
    expect(messagePreviewText('')).toBe('')
  })
})
