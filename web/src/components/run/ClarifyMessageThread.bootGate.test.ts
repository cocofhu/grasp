// @vitest-environment happy-dom
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { __resetConfirmFlowWindowForTests, confirmFlowWindowVisible } from '@/lib/inbox/confirmFlowWindow'
import { beginBoot, bootKey, clearBootRecords, markBootLive } from '@/lib/run/bootRecord'
import ClarifyMessageThread from './ClarifyMessageThread.vue'

const KEY = bootKey('run-1', 'implement', 1)

function mountThread(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(ClarifyMessageThread, {
    props: {
      runId: 'run-1',
      nodeId: 'implement',
      nodeTask: '上游交接说明',
      bootRecordKey: KEY,
      turns: [
        { role: 'human', text: '当前这一步的产物已经写好', at: '2026-10-21T05:00:00Z' },
        {
          role: 'agent',
          text: '已连接，开始执行这一步。',
          at: '2026-10-21T05:00:01Z',
          parts: [{ kind: 'message', text: '已连接，开始执行这一步。' }],
        },
      ],
      ...props,
    },
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

afterEach(() => {
  clearBootRecords()
  __resetConfirmFlowWindowForTests()
})

describe('ClarifyMessageThread boot gate', () => {
  it('keeps this boot’s handoff and reply under 环境已就绪, and off the in-progress checklist', async () => {
    clearBootRecords()
    beginBoot(KEY, '2026-10-21T05:00:00Z')
    const w = mountThread({ bootStarting: true, bootSummary: false })
    expect(w.get('[data-testid="clarify-boot-title"]').text()).toBe('正在启动 Agent')
    expect(w.text()).toContain('当前这一步的产物已经写好')
    expect(w.text()).not.toContain('已连接，开始执行这一步。')
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)

    confirmFlowWindowVisible.value = true
    await nextTick()
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(false)
    expect(w.text()).toContain('当前这一步的产物已经写好')

    confirmFlowWindowVisible.value = false
    markBootLive(KEY)
    await w.setProps({ bootStarting: false, bootSummary: true })
    const html = w.html()
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(false)
    expect(html.indexOf('clarify-boot-done')).toBeLessThan(html.indexOf('clarify-node-task'))
    expect(html.indexOf('clarify-node-task')).toBeLessThan(html.indexOf('clarify-agent-message'))
    w.unmount()
  })
})
