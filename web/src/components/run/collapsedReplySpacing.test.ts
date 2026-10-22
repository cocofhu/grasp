import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const timeline = readFileSync(join(here, 'AgentTimeline.vue'), 'utf8')
const chat = readFileSync(join(here, '../../styles/chat.css'), 'utf8')

function block(css: string, selector: string): string {
  const found = css.match(new RegExp(`${selector}\\s*\\{[^}]*\\}`, 's'))
  expect(found, selector).not.toBeNull()
  return found![0]
}

describe('collapsed reply spacing', () => {
  const style = timeline.slice(timeline.indexOf('<style scoped>'))

  it('keeps the elapsed row and hairline on the shared timeline styles', () => {
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
    expect(style).toContain('.agent-elapsed,\n  .agent-thought-summary { min-height: 44px; }')
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
})
