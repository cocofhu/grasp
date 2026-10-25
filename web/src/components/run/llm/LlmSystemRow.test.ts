// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import LlmSystemRow from './LlmSystemRow.vue'

function mountRow(props: { event: string; nodeLabel: string; toLabel?: string; edgeKind?: string }) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages } } })
  return mount(LlmSystemRow, { props, global: { plugins: [i18n] } })
}

describe('LlmSystemRow', () => {
  it('uses arrow-flow for a move, and the error tone on a failure branch', () => {
    const flow = mountRow({ event: 'transition', nodeLabel: '实现', toLabel: '测试' })
    expect(flow.get('[data-testid="llm-system-row"]').attributes('data-icon')).toBe('arrow-flow')
    expect(flow.text()).toContain('流转到「测试」')
    expect(flow.html()).not.toContain('chevron-right')
    expect(flow.get('svg').classes()).toContain('text-txt3')
    flow.unmount()

    const bad = mountRow({ event: 'transition', nodeLabel: '实现', toLabel: '修复', edgeKind: 'failure' })
    expect(bad.get('[data-testid="llm-system-row"]').attributes('data-icon')).toBe('arrow-flow')
    expect(bad.text()).toContain('走失败分支到「修复」')
    expect(bad.get('svg').classes()).toContain('text-err')
    expect(bad.html()).not.toContain('chevron-right')
    bad.unmount()
  })

  it('uses user-hold for a person, and quiet-dot when a node ends', () => {
    const hold = mountRow({ event: 'pause', nodeLabel: '审批' })
    expect(hold.get('[data-testid="llm-system-row"]').attributes('data-icon')).toBe('user-hold')
    expect(hold.text()).toContain('「审批」等待人工处理')
    expect(hold.html()).not.toContain('M12 3l8 4v5')
    expect(hold.get('svg').classes()).toContain('text-warn')
    hold.unmount()

    const ended = mountRow({ event: 'exit', nodeLabel: '实现' })
    expect(ended.get('[data-testid="llm-system-row"]').attributes('data-icon')).toBe('quiet-dot')
    expect(ended.text()).toContain('「实现」结束')
    ended.unmount()
  })

  it('uses history-back and play-continue without changing the sentence', () => {
    const back = mountRow({ event: 'rollback', nodeLabel: '评审', toLabel: '实现' })
    expect(back.get('[data-testid="llm-system-row"]').attributes('data-icon')).toBe('history-back')
    expect(back.text()).toContain('退回到「实现」')
    back.unmount()

    const go = mountRow({ event: 'resume', nodeLabel: '实现' })
    expect(go.get('[data-testid="llm-system-row"]').attributes('data-icon')).toBe('play-continue')
    expect(go.text()).toContain('「实现」继续执行')
    expect(go.get('svg').classes()).toContain('text-accent-2')
    go.unmount()
  })

  it('does not draw a row for enter', () => {
    const w = mountRow({ event: 'enter', nodeLabel: '实现' })
    expect(w.find('[data-testid="llm-system-row"]').exists()).toBe(false)
    w.unmount()
  })
})
