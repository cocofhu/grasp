// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyPriorSegment } from './ClarifyChat.vue'
import ClarifyMessageThread from './ClarifyMessageThread.vue'

const prior: ClarifyPriorSegment = {
  nodeId: 'clarify',
  iteration: 1,
  label: '澄清',
  kind: 'chat',
  status: 'completed',
  at: '2026-10-01T00:00:00Z',
  endedAt: '2026-10-01T00:00:31Z',
  open: true,
  turns: [
    { role: 'human', text: '第一问', at: '2026-10-01T00:00:01Z' },
    { role: 'agent', text: '第一答', at: '2026-10-01T00:00:05Z' },
    { role: 'human', text: '第二问', at: '2026-10-01T00:00:20Z' },
    { role: 'agent', text: '第二答', at: '2026-10-01T00:00:25Z' },
  ],
  notes: [
    { key: 'pause', event: 'pause', nodeLabel: '澄清', afterTurn: 1 },
    { key: 'resume', event: 'resume', nodeLabel: '澄清', afterTurn: 1 },
    { key: 'exit', event: 'exit', nodeLabel: '澄清', afterTurn: 3 },
    { key: 'transition', event: 'transition', nodeLabel: '澄清', toLabel: '门禁', afterTurn: 3 },
  ],
}

function marks(root: Element): string[] {
  return [...root.querySelectorAll('[data-testid="clarify-prior-summary"], [data-turn-role], [data-testid="llm-system-row"], [data-testid="clarify-segment-divider"]')].map((el) => {
    const testId = el.getAttribute('data-testid')
    if (testId === 'clarify-prior-summary') return 'summary'
    if (testId === 'clarify-segment-divider') return 'divider'
    if (testId === 'llm-system-row') return el.getAttribute('data-event') || ''
    return `turn:${el.getAttribute('data-turn-role')}`
  })
}

describe('ClarifyMessageThread trace order', () => {
  it('draws pause and resume between rounds, and exit plus transition after the visit', () => {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
    const wrapper = mount(ClarifyMessageThread, {
      props: {
        runId: 'r1',
        priorSegments: [prior],
        turns: [],
        currentNotes: [{ key: 'wait', event: 'pause', nodeLabel: '门禁', afterTurn: -1 }],
        nodeLabel: '门禁',
      },
      global: { plugins: [i18n] },
    })
    expect(marks(wrapper.element)).toEqual([
      'summary',
      'turn:human',
      'turn:agent',
      'pause',
      'resume',
      'turn:human',
      'turn:agent',
      'exit',
      'transition',
      'divider',
      'pause',
    ])
    wrapper.unmount()
  })
})
