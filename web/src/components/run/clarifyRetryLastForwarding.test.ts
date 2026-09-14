// @vitest-environment happy-dom
/**
 * Empty-fail Retry must survive the wrapper chain, not just ClarifyChat.
 *
 * The views bind `@retry-last` on the wrapper (RunClarifyPanel / RunReviewPanel /
 * ReviewComposer), so a wrapper that forgets to relay the event leaves a visible
 * but dead button. Mount the REAL ClarifyChat here — stubbing it is exactly how
 * the dropped relay stayed green.
 */
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { i18n as globalI18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
import type { ClarifyTurn, NodeRun, Run, WFNode } from '@/lib/shared/types'
import RunClarifyPanel from './RunClarifyPanel.vue'
import RunReviewPanel from './RunReviewPanel.vue'
import ReviewComposer from './ReviewComposer.vue'

const RETRY = '[data-testid="clarify-empty-fail-retry"]'

/** Trailing empty agent after a human — the only retryable shape. */
const emptyFailTurns: ClarifyTurn[] = [
  { role: 'human', text: '这个按钮有问题', at: 't1' },
  { role: 'agent', text: '', at: 't2' },
]

beforeAll(async () => {
  // ClarifyChat's relTime() reads the global i18n instance.
  globalI18n.global.setLocaleMessage('zh-CN', await loadLocaleMessages('zh-CN'))
  globalI18n.global.locale.value = 'zh-CN'
})

function plugins() {
  return [
    createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    }),
  ]
}

/** ReviewShell renders stage/sidebar slots; ClarifyChat lives in the sidebar. */
const ReviewShellStub = defineComponent({
  template: '<div><slot name="stage" /><slot name="sidebar" /></div>',
})

const stubs = {
  ReviewShell: ReviewShellStub,
  ReactArtifactStage: true,
  ReactConnectingState: true,
  Icon: true,
  ClarifyDemoFrame: true,
}

const stubRun = {
  id: 'run-1',
  workflowId: 'wf-1',
  workflowName: 'wf',
  status: 'waiting_human',
  trigger: 'manual',
  startedAt: '2026-09-14T00:00:00Z',
  nodeRuns: {},
  artifacts: [],
} as unknown as Run

const stubNode = {
  id: 'approve_7gl6',
  type: 'approve',
  label: '澄清',
  position: { x: 0, y: 0 },
  config: {},
} as WFNode

const stubNodeRun = { nodeId: 'approve_7gl6', status: 'waiting_human' } as unknown as NodeRun

const clarify = { nodeId: 'approve_7gl6', iteration: 2, turns: emptyFailTurns, done: false }

describe('empty-fail retry survives the wrapper chain', () => {
  it('RunClarifyPanel relays retry-last from a real ClarifyChat', async () => {
    const wrapper = mount(RunClarifyPanel, {
      props: {
        sandboxFailed: false,
        nodeLabel: '澄清',
        nodeId: 'approve_7gl6',
        clarify,
        runId: 'run-1',
        run: stubRun,
        draft: '',
        attachments: [],
        annotations: [],
        inputActive: true,
        selStatus: 'waiting_human',
      },
      global: { plugins: plugins(), stubs },
    })

    const retry = wrapper.find(RETRY)
    expect(retry.exists()).toBe(true)
    expect((retry.element as HTMLButtonElement).disabled).toBe(false)

    await retry.trigger('click')
    expect(wrapper.emitted('retry-last')).toHaveLength(1)
    wrapper.unmount()
  })

  it('ReviewComposer relays retry-last from a real ClarifyChat', async () => {
    const wrapper = mount(ReviewComposer, {
      props: {
        mode: 'clarify',
        runId: 'run-1',
        nodeId: 'approve_7gl6',
        iteration: 2,
        turns: emptyFailTurns,
        nodeType: 'approve',
        done: false,
        active: true,
      },
      global: { plugins: plugins(), stubs },
    })

    const retry = wrapper.find(RETRY)
    expect(retry.exists()).toBe(true)

    await retry.trigger('click')
    expect(wrapper.emitted('retry-last')).toHaveLength(1)
    wrapper.unmount()
  })

  // RunReviewPanel -> ReviewComposer -> ClarifyChat: both hops must relay.
  it('RunReviewPanel relays retry-last through nested ReviewComposer', async () => {
    const wrapper = mount(RunReviewPanel, {
      props: {
        mobile: false,
        node: stubNode,
        nodeRun: stubNodeRun,
        run: stubRun,
        clarify,
        draft: '',
        attachments: [],
        annotations: [],
        inputActive: true,
        selStatus: 'waiting_human',
      },
      global: { plugins: plugins(), stubs },
    })

    const retry = wrapper.find(RETRY)
    expect(retry.exists()).toBe(true)

    await retry.trigger('click')
    expect(wrapper.emitted('retry-last')).toHaveLength(1)
    wrapper.unmount()
  })
})
