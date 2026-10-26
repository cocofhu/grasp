// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Run } from '@/lib/shared/types'
import RunBoardCard from './RunBoardCard.vue'

function sampleRun(): Run {
  return {
    id: 'run-abc123',
    title: '测试运行',
    workflowId: 'wf-1',
    workflowName: 'demo',
    status: 'running',
    progress: 40,
    currentNodeLabel: '调研',
    createdAt: '2026-07-18T00:00:00Z',
    nodes: [],
    edges: [],
    nodeStates: {},
    artifacts: [],
  } as unknown as Run
}

describe('RunBoardCard', () => {
  it('renders run title and emits select on click', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const run = sampleRun()
    const wrapper = mount(RunBoardCard, {
      props: { run },
      global: { plugins: [i18n], stubs: { StatusPill: { template: '<span />' } } },
    })
    expect(wrapper.text()).toContain('测试运行')
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual([run])
    expect(wrapper.get('button').classes()).toContain('bg-transparent')
    expect(wrapper.get('button').classes()).toContain('hover:bg-elevated')
    expect(wrapper.get('button').classes()).not.toContain('bg-elevated')
    wrapper.unmount()
  })

  it('wraps a long title and keeps one hover treatment for every status', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const title = '标'.repeat(80)
    const mountCard = (status: Run['status']) => mount(RunBoardCard, {
      props: { run: { ...sampleRun(), status, title, priority: status === 'running' ? 'high' : 'normal' } },
      global: { plugins: [i18n], stubs: { StatusPill: { template: '<span />' } } },
    })
    const running = mountCard('running')
    const waiting = mountCard('waiting_human')
    const done = mountCard('completed')
    for (const wrapper of [running, waiting, done]) {
      expect(wrapper.text()).toContain(title)
      expect(wrapper.get('button').classes()).toContain('hover:bg-elevated')
    }
    expect(running.text()).toContain('高')
    expect(done.text()).not.toContain('高')
    running.unmount()
    waiting.unmount()
    done.unmount()
  })

  it('shows clock duration for completed runs and a real progress width otherwise', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const run = { ...sampleRun(), status: 'completed' as const, durationSec: 3458, progress: 100 }
    const wrapper = mount(RunBoardCard, {
      props: { run },
      global: { plugins: [i18n], stubs: { StatusPill: { template: '<span />' } } },
    })
    expect(wrapper.text()).toContain('57:38')
    expect(wrapper.text()).not.toMatch(/\d+h|\d+时/)
    expect(wrapper.find('.run-board-progress').exists()).toBe(false)
    await wrapper.setProps({ run: { ...run, durationSec: 3703 } })
    expect(wrapper.text()).toContain('01:01:43')
    await wrapper.setProps({ run: { ...sampleRun(), progress: 40, status: 'running' } })
    expect(wrapper.get('.run-board-progress').attributes('style')).toContain('width: 40%')
    expect(wrapper.html()).not.toContain('animate-')
    await wrapper.setProps({ run: { ...sampleRun(), progress: 0, status: 'waiting_human' } })
    expect(wrapper.get('.run-board-progress').attributes('style')).toContain('width: 0%')
    wrapper.unmount()
  })
})
