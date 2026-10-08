// @vitest-environment happy-dom
/**
 * Acceptance probes: ClarifyChat reviewMode queue / stream / Cancel
 * (shared by visual + research node-inline review via ReviewComposer).
 */
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { describe, expect, it, beforeEach } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ClarifyChat from './ClarifyChat.vue'

function mountReview(nodeId: string) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ClarifyChat, {
    props: {
      runId: 'run-1',
      nodeId,
      iteration: 1,
      turns: [],
      done: false,
      active: true,
      reviewMode: true,
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ClarifyDemoFrame: true },
    },
  })
}

async function sendText(wrapper: ReturnType<typeof mountReview>, text: string) {
  await wrapper.find('textarea').setValue(text)
  const sendBtn = wrapper.find('button[class*="bg-accent"]')
  expect(sendBtn.exists()).toBe(true)
  await sendBtn.trigger('click')
  await flushPromises()
}

describe('[approving] ClarifyChat reviewMode UX (visual/research shared)', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  it('research surface: enqueue shows queue panel; confirm disabled before turn_begin', async () => {
    const w = mountReview('research')
    await sendText(w, '方案意见甲')
    await sendText(w, '方案意见乙')
    const queue = w.find('[data-testid="clarify-review-queue"]')
    expect(queue.exists()).toBe(true)
    expect(queue.text()).toMatch(/待发送队列/)
    expect(queue.text()).toContain('方案意见甲')
    expect(queue.text()).toContain('方案意见乙')
    const confirm = w.find('[data-testid="clarify-confirm-flow"]')
    expect((confirm.element as HTMLButtonElement).disabled).toBe(true)
    // No transcript human/agent bubbles until turn_begin (persisted turns empty).
    expect(w.findAll('.rounded-2xl').filter((n) => n.text().includes('方案意见甲')).length).toBe(0)
    w.unmount()
  })

  it('visual surface: turn_begin + acp stream + Cancel clears queue / interrupted', async () => {
    const w = mountReview('visual')
    await sendText(w, '视觉意见1')
    await sendText(w, '视觉意见2')
    const vm = w.vm as unknown as {
      applyReviewFrame: (f: Record<string, unknown>) => void
      applyAcpEvents: (e: { kind: string; text: string }[]) => void
    }
    vm.applyReviewFrame({ event: 'turn_begin', item: { text: '视觉意见1' }, nodeId: 'visual' })
    await nextTick()
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toContain('视觉意见2')
    expect(w.text()).toContain('视觉意见1')
    vm.applyAcpEvents([{ kind: 'message', text: '增量流式正文' }])
    await nextTick()
    expect(w.text()).toContain('增量流式正文')
    const cancel = w.find('[data-testid="clarify-review-cancel"]')
    expect(cancel.exists()).toBe(true)
    expect(cancel.attributes('aria-label')).toBe('Cancel')
    expect(cancel.attributes('title')).toBe('Cancel')
    expect(cancel.text()).not.toContain('Cancel')
    expect(cancel.findComponent({ name: 'Icon' }).props('name')).toBe('stop')
    await cancel.trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-interrupted"]').exists()).toBe(true)
    const confirm = w.find('[data-testid="clarify-confirm-flow"]')
    expect((confirm.element as HTMLButtonElement).disabled).toBe(false)
    w.unmount()
  })

  it('enqueue failure: discardLastQueued restores confirm gate (v1)', async () => {
    const w = mountReview('research')
    await sendText(w, '将失败的意见')
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(true)
    expect((w.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
      true,
    )
    const vm = w.vm as unknown as { discardLastQueued: () => void }
    vm.discardLastQueued()
    await nextTick()
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(false)
    expect((w.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
      false,
    )
    w.unmount()
  })

  it('remote Cancel via queue_state waiting=0 clears ghost queue (v2 FR5)', async () => {
    const w = mountReview('visual')
    await sendText(w, '页A意见1')
    await sendText(w, '页A意见2')
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toContain('页A意见1')
    const vm = w.vm as unknown as { applyReviewFrame: (f: Record<string, unknown>) => void }
    // Simulate another entry CancelReviewSession → queue_state waiting=0.
    vm.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'visual',
      waiting: 0,
      items: [],
    })
    await nextTick()
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(false)
    expect((w.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(
      false,
    )
    w.unmount()
  })

  it('ignores review frames for other nodeId (v3)', async () => {
    const w = mountReview('research')
    await sendText(w, '本节点意见')
    const vm = w.vm as unknown as { applyReviewFrame: (f: Record<string, unknown>) => void }
    vm.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'other-node',
      waiting: 0,
      items: [],
    })
    await nextTick()
    // Wrong nodeId must not clear this session's queue.
    expect(w.find('[data-testid="clarify-review-queue"]').text()).toContain('本节点意见')
    w.unmount()
  })
})
