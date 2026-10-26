import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const timeline = readFileSync(join(here, 'AgentTimeline.vue'), 'utf8')
const tools = readFileSync(join(here, 'agentToolGroup.css'), 'utf8')
const chat = readFileSync(join(here, '../../styles/chat.css'), 'utf8')
const status = readFileSync(join(here, 'ThoughtSummaryStatus.vue'), 'utf8')

function block(css: string, selector: string): string {
  const found = css.match(new RegExp(`${selector}\\s*\\{[^}]*\\}`, 's'))
  expect(found, selector).not.toBeNull()
  return found![0]
}

describe('collapsed reply spacing', () => {
  const style = timeline.slice(timeline.indexOf('<style scoped>'))

  it('locks process fold rows at 20px and only raises them for a coarse pointer', () => {
    const thought = block(style, '\\.agent-thought-summary')
    expect(thought).toContain('height: 20px')
    expect(thought).toContain('min-height: 20px')
    expect(thought).toContain('max-height: 20px')
    expect(thought).toContain('line-height: 20px')
    expect(thought).not.toContain('min-height: 32px')

    const chatSummary = block(chat, '\\.chat-thought-summary')
    expect(chatSummary).toContain('height: 20px')
    expect(chatSummary).toContain('min-height: 20px')
    expect(chatSummary).toContain('max-height: 20px')

    const toolsHead = block(tools, '\\.agent-tools-head')
    expect(toolsHead).toContain('height: 20px')
    expect(toolsHead).toContain('min-height: 20px')
    expect(toolsHead).toContain('max-height: 20px')

    expect(style).toMatch(/@media \(pointer: coarse\)/)
    expect(style).toMatch(/\.agent-thought-summary \{ height: 44px; min-height: 44px; max-height: 44px;/)
    expect(chat).toMatch(/@media \(pointer: coarse\)[\s\S]*\.chat-thought-summary \{ height: 44px; min-height: 44px; max-height: 44px;/)
    expect(tools).toMatch(/@media \(pointer: coarse\)[\s\S]*\.agent-tools-head \{ height: 44px; min-height: 44px; max-height: 44px;/)

    expect(style).not.toMatch(/@media \(max-width:\s*767px\)[^{]*\{[^}]*\.agent-thought-summary/)
    expect(chat).not.toMatch(/@media \(max-width:\s*767px\)[^{]*\{[^}]*\.chat-thought-summary/)
    expect(tools).not.toMatch(/@media \(max-width:\s*767px\)[^{]*\{[^}]*\.agent-tools-head/)

    expect(style).toContain('.agent-thought { margin: 0 0 2px;')
    expect(chat).toContain('.chat-thought { margin: 0 0 2px; }')
    expect(tools).toContain('margin: 0 0 2px;')
    expect(status).toContain(':size="12"')
    expect(status).toContain('width: 12px; height: 12px;')
    expect(status).not.toContain(':size="16"')
    expect(status).not.toContain('width: 16px; height: 16px;')

    const elapsed = block(style, '\\.agent-elapsed')
    expect(elapsed).toContain('min-height: 24px')
    expect(elapsed).toContain('font-size: 13px')
    expect(elapsed).toContain('line-height: 1')
    expect(elapsed).toContain('letter-spacing: 0.01em')
    expect(elapsed).toContain('color: rgb(var(--c-txt2))')
    expect(elapsed).not.toContain('13.5px')
    expect(elapsed).not.toContain('--c-txt3')

    expect(block(style, '\\.agent-elapsed-chevron')).toContain('color: rgb(var(--c-txt2))')
    expect(style).toMatch(/\.agent-elapsed:hover\s*\{[^}]*color:\s*rgb\(var\(--c-txt\)\)/)
    expect(style).toMatch(/\.agent-elapsed:hover \.agent-elapsed-chevron\s*\{[^}]*color:\s*rgb\(var\(--c-txt\)\)/)
    expect(style).toMatch(/\[aria-expanded='true'\] \.agent-elapsed-chevron\s*\{[^}]*transform:\s*rotate\(90deg\)/)

    const rule = block(style, '\\.agent-elapsed-rule')
    expect(rule).toContain('height: 1px')
    expect(rule).toContain('margin: 4px 0 8px')
    expect(rule).toContain('background: rgb(var(--c-line))')
  })

  it('renders thought as 12.5px / 1.45 paragraphs with an 8px gap', () => {
    for (const [css, selector] of [
      [style, '\\.agent-thought-body'],
      [style, '\\.agent-elapsed-thought'],
      [chat, '\\.chat-thought-body'],
    ] as const) {
      const body = block(css, selector)
      expect(body).toContain('font-size: 12.5px')
      expect(body).toContain('line-height: 1.45')
      expect(body).not.toContain('line-height: 1.8')
      expect(body).not.toContain('line-height: 1.75')
      expect(body).not.toContain('whitespace-pre-wrap')
      expect(css).toMatch(new RegExp(`${selector} p\\s*\\{[^}]*margin:\\s*0 0 8px`))
    }
    expect(timeline).not.toMatch(/agent-thought-body whitespace-pre-wrap/)
    expect(timeline).not.toMatch(/agent-elapsed-thought whitespace-pre-wrap/)
    expect(timeline).toContain('thoughtParagraphs(')
  })

  it('uses the Codex prose rhythm and leaves other blocks alone', () => {
    const prose = block(chat, '\\.chat-prose\\.md,\\s*\\.chat-thread \\.chat-prose,\\s*\\.chat-prose \\.md')
    expect(prose).toContain('font-size: 15px')
    expect(prose).toContain('line-height: 23px')
    expect(prose).not.toContain('line-height: 1.65')
    expect(prose).toContain('color: rgb(var(--c-txt))')
    expect(prose).not.toContain('1.85')

    expect(chat).toMatch(/\.chat-thread \.chat-prose p\s*\{\s*margin:\s*0 0 11px;\s*\}/)
    expect(chat).toMatch(/\.chat-thread \.chat-prose :is\(p, ul, ol, h1, h2, h3, h4, pre, blockquote\):first-child\s*\{\s*margin-top:\s*0;\s*\}/)
    expect(chat).toMatch(/\.chat-thread \.chat-prose :is\(p, ul, ol, pre, blockquote\):last-child\s*\{\s*margin-bottom:\s*0;\s*\}/)

    const lists = block(chat, '\\.chat-thread \\.chat-prose :is\\(ul, ol\\)')
    expect(lists).toContain('margin: 10px 0')
    expect(lists).toContain('padding-left: 1.5em')
    expect(chat).toMatch(/\.chat-thread \.chat-prose li \+ li\s*\{\s*margin-top:\s*8px;\s*\}/)
    expect(chat).not.toMatch(/\.chat-thread \.chat-prose p\s*\{[^}]*4px/)
    expect(chat).not.toMatch(/:lang\(zh(?:-CN)?\)/)

    const user = block(chat, '\\.chat-thread \\.chat-user-message')
    expect(user).toContain('font-size: 15px')
    expect(user).toContain('line-height: 1.75')
    expect(block(chat, '\\.chat-thread \\.chat-prose :is\\(h1, h2, h3, h4\\)')).toContain('margin-block: 1.35em .5em')
    expect(block(chat, '\\.chat-thread \\.chat-prose pre')).toContain('margin-block: 1em')
    expect(block(chat, '\\.chat-thread \\.chat-prose pre')).toContain('line-height: 1.75')
    expect(block(chat, '\\.chat-thread \\.chat-prose blockquote')).toContain('margin-block: 1em')
    expect(block(chat, '\\.chat-thread \\.chat-prose table')).toContain('margin-block: 1em')
    expect(block(chat, '\\.chat-thread \\.chat-prose hr')).toContain('margin-block: 1.5em')
  })

  it('uses a 2px block gap and lines the thought summary up with the tool row', () => {
    expect(timeline).not.toContain('mb-3')
    expect(timeline).toContain('agent-live-message')
    expect(style).toContain('.agent-live-message { margin: 0 0 2px;')
    expect(style).toContain('.agent-live-message:last-child { margin-bottom: 0; }')

    const elapsedThought = block(style, '\\.agent-elapsed-thought')
    expect(elapsedThought).toContain('margin: 0 0 2px')
    expect(elapsedThought).not.toContain('8px')
    expect(style).toContain('.agent-elapsed-note { margin-bottom: 2px; }')
    expect(style).not.toContain('margin-bottom: 8px;')
    expect(style).not.toContain('margin-bottom: 4px;')

    const detail = block(tools, '\\.agent-tool-detail')
    expect(detail).toContain('margin: 2px 0 2px 28px')
    expect(detail).not.toContain('10px')

    expect(style).toContain('.agent-thought-summary::marker { content: none; }')
    expect(style).toContain('.agent-thought-summary::-webkit-details-marker { display: none; }')

    expect(chat).toMatch(/\.agent-timeline :is\(\.agent-live-message, \.agent-elapsed-note\)/)
    expect(chat).toMatch(/\.agent-timeline \.agent-live-message \.stream-md-block:last-child/)
    expect(chat).toMatch(/\.chat-thread \.chat-prose p\s*\{\s*margin:\s*0 0 11px;\s*\}/)
  })
})
