// @vitest-environment happy-dom
import { defineComponent, h, onMounted } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import pages from '@/locales/zh-CN/pages.json'
import ReviewShell from './ReviewShell.vue'
import { useConversationArtifacts, type ConversationArtifacts } from '@/lib/chat/conversationArtifacts'
import { registerStageLinks } from '@/lib/run/stageLinks'
import { ensureConversationDrawer, readArtifactPanelOpen, resetConversationDrawers, writeArtifactPanelOpen } from '@/lib/chat/conversationDrawer'

enableAutoUnmount(afterEach)
afterEach(() => { document.body.replaceChildren(); localStorage.clear(); resetConversationDrawers() })

function mountShell(props: { scopeKey?: string; runId?: string; chatOnly?: boolean; hostConfirmFlow?: boolean; splitDrawer?: boolean; rememberArtifactPanel?: boolean; runName?: string } = {}) {
  const mountedChat = vi.fn()
  const mountedStage = vi.fn()
  let artifacts: ConversationArtifacts | null = null
  const Chat = defineComponent({
    setup() {
      artifacts = useConversationArtifacts()
      onMounted(mountedChat)
      return () => h('input', { 'data-testid': 'chat-draft', value: '保留草稿' })
    },
  })
  const Stage = defineComponent({
    setup() { onMounted(mountedStage); return () => h('div', { 'data-testid': 'stage-slot' }, '产物') },
  })
  const wrapper = mount(ReviewShell, {
    props: { scopeKey: 'run:node:1', runId: 'run-1', ...props },
    slots: { sidebar: () => h(Chat), stage: () => h(Stage) },
    attachTo: document.body,
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })] },
  })
  return { wrapper, mountedChat, mountedStage, openArtifact: (target: Parameters<ConversationArtifacts['open']>[0]) => artifacts!.open(target) }
}

describe('ReviewShell toolbar run name', () => {
  it('shows a passed run name and keeps 对话 when the name is omitted', async () => {
    const named = mountShell({ runName: '优化这里的段间距' })
    expect(named.wrapper.get('[data-testid="conversation-title"]').text()).toBe('优化这里的段间距')
    expect(named.wrapper.get('[data-testid="conversation-title"]').attributes('title')).toBe('优化这里的段间距')
    await named.wrapper.setProps({ runName: '丙' })
    expect(named.wrapper.get('[data-testid="conversation-title"]').text()).toBe('丙')
    const plain = mountShell()
    expect(plain.wrapper.get('[data-testid="conversation-title"]').text()).toBe('对话')
    expect(plain.wrapper.find('[data-testid="conversation-title-skeleton"]').exists()).toBe(false)
  })

  it('does not render the toolbar for chat-only, even with a run name', () => {
    const host = mountShell({ chatOnly: true, runName: '不会出现' })
    expect(host.wrapper.find('.conversation-toolbar').exists()).toBe(false)
    expect(host.wrapper.find('[data-testid="conversation-title"]').exists()).toBe(false)
    expect(host.wrapper.find('[data-testid="conversation-title-skeleton"]').exists()).toBe(false)
  })

  it('shows the placeholder bar for an empty run name', () => {
    const host = mountShell({ runName: '' })
    expect(host.wrapper.find('[data-testid="conversation-title-skeleton"]').exists()).toBe(true)
    expect(host.wrapper.find('[data-testid="conversation-title"]').exists()).toBe(false)
    expect(host.wrapper.get('.conversation-heading').text()).not.toContain('对话')
  })
})

describe('ReviewShell conversation layout', () => {
  it('starts with the conversation and keeps both the draft and product mounted while toggling', async () => {
    const { wrapper, mountedChat, mountedStage } = mountShell()
    expect(wrapper.find('[data-testid="conversation-resource-toggle"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="review-shell-sidebar"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(false)
    expect(mountedStage).toHaveBeenCalledOnce()
    await wrapper.get('[data-testid="chat-draft"]').setValue('尚未发送')
    await wrapper.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    expect(wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(true)
    await wrapper.get('[data-testid="conversation-panel-close"]').trigger('click')
    expect((wrapper.get('[data-testid="chat-draft"]').element as HTMLInputElement).value).toBe('尚未发送')
    expect(mountedChat).toHaveBeenCalledOnce()
    expect(mountedStage).toHaveBeenCalledOnce()
  })

  it('opens the existing scoped stage from artifact/tool references and closes on a new execution scope', async () => {
    const openArtifact = vi.fn()
    const openPreview = vi.fn()
    const unregister = registerStageLinks('run-1', { hasArtifact: () => true, openArtifact, canOpenPreview: () => true, openPreview })
    try {
      const host = mountShell()
      await host.openArtifact({ name: 'review.json', runId: 'run-1' })
      expect(host.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(true)
      expect(openArtifact).toHaveBeenCalledWith('review.json')
      await host.openArtifact({ preview: true })
      expect(openPreview).toHaveBeenCalledOnce()
      await host.wrapper.setProps({ scopeKey: 'run:node:2' })
      await flushPromises()
      expect(host.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(false)
    } finally { unregister() }
  })

  it('keeps embedded chat-only credentials away from product mounting or artifact controls', async () => {
    const host = mountShell({ chatOnly: true })
    expect(host.wrapper.find('[data-testid="conversation-panel-toggle"]').exists()).toBe(false)
    expect(host.wrapper.find('[data-testid="review-shell-stage"]').exists()).toBe(false)
    expect(host.mountedStage).not.toHaveBeenCalled()
    await host.openArtifact({ name: 'private.json' })
    expect(host.wrapper.find('[data-testid="conversation-artifact-panel"]').exists()).toBe(false)
    expect(host.mountedChat).toHaveBeenCalledOnce()
  })

  it('does not open another registered run or borrow a sole stage without an explicit host run', async () => {
    const openArtifact = vi.fn()
    const unregister = registerStageLinks('other-run', { hasArtifact: () => true, openArtifact, canOpenPreview: () => false, openPreview: vi.fn() })
    try {
      const host = mountShell()
      await host.openArtifact({ runId: 'other-run', name: 'private.json' })
      expect(host.wrapper.get('[data-testid="conversation-panel-toggle"]').attributes('aria-expanded')).toBe('false')
      const unscoped = mountShell({ runId: '' })
      await unscoped.openArtifact({ name: 'private.json' })
      expect(unscoped.wrapper.get('[data-testid="conversation-panel-toggle"]').attributes('aria-expanded')).toBe('false')
      expect(openArtifact).not.toHaveBeenCalled()
    } finally { unregister() }
  })

  it('retains the confirmation host contract and permits the outer GateApproval host to own it', () => {
    const ordinary = mountShell()
    expect(ordinary.wrapper.find('[data-testid="confirm-flow-overlay"]').exists()).toBe(true)
    expect((ordinary.wrapper.vm as { playConfirmCeremony: unknown }).playConfirmCeremony).toBeTypeOf('function')
    const gate = mountShell({ hostConfirmFlow: false })
    expect(gate.wrapper.find('[data-testid="confirm-flow-overlay"]').exists()).toBe(false)
  })

  it('opens one sandbox drawer from icon buttons and drops it when the conversation changes', async () => {
    const entry = ensureConversationDrawer('run-1')
    const host = mountShell({ splitDrawer: true })
    expect(host.wrapper.find('[data-testid="conversation-resource-toggle"]').exists()).toBe(true)
    const artifact = host.wrapper.get('[data-testid="conversation-panel-toggle"]')
    expect(artifact.text()).not.toContain('产物')
    await host.wrapper.get('[data-testid="conversation-resource-toggle"]').trigger('click')
    expect(host.wrapper.get('[data-testid="conversation-artifact-panel"]').attributes('data-drawer-mode')).toBe('resources')
    await artifact.trigger('click')
    expect(artifact.attributes('aria-pressed')).toBe('true')
    expect(host.wrapper.get('[data-testid="conversation-resource-toggle"]').attributes('aria-pressed')).toBe('false')
    const epoch = entry!.resetEpoch
    await host.wrapper.setProps({ scopeKey: 'run:node:2' })
    await flushPromises()
    expect(host.wrapper.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(artifact.attributes('aria-pressed')).toBe('false')
    expect(entry!.resetEpoch).toBe(epoch + 1)
    const embedded = mountShell({ splitDrawer: true, chatOnly: true })
    expect(embedded.wrapper.find('[data-testid="conversation-resource-toggle"]').exists()).toBe(false)
    expect(embedded.wrapper.find('[data-testid="conversation-panel-toggle"]').exists()).toBe(false)
  })

  it('opens an artifact reference on the artifact icon and a preview on the resource icon', async () => {
    const openArtifact = vi.fn()
    const openPreview = vi.fn()
    const unregister = registerStageLinks('run-1', { hasArtifact: () => true, openArtifact, canOpenPreview: () => true, openPreview })
    try {
      const host = mountShell({ splitDrawer: true })
      await host.openArtifact({ name: 'review.json', runId: 'run-1' })
      expect(host.wrapper.get('[data-testid="conversation-panel-toggle"]').attributes('aria-pressed')).toBe('true')
      expect(host.wrapper.get('[data-testid="conversation-artifact-panel"]').attributes('data-drawer-mode')).toBe('artifacts')
      expect(openArtifact).toHaveBeenCalledWith('review.json')
      await host.openArtifact({ preview: true, runId: 'run-1' })
      expect(host.wrapper.get('[data-testid="conversation-resource-toggle"]').attributes('aria-pressed')).toBe('true')
      expect(host.wrapper.get('[data-testid="conversation-panel-toggle"]').attributes('aria-pressed')).toBe('false')
      expect(openPreview).toHaveBeenCalledOnce()
    } finally { unregister() }
  })
})

describe('ReviewShell artifact panel session', () => {
  it('stays open across pending and recent scopes, stays closed after the user closes, and does not open the previous file', async () => {
    const openArtifact = vi.fn()
    const openPreview = vi.fn()
    const links = { hasArtifact: () => true, openArtifact, canOpenPreview: () => true, openPreview }
    const unregisterPending = registerStageLinks('run-pending', links)
    const unregisterRecent = registerStageLinks('run-recent', links)
    try {
      const pending = mountShell({
        rememberArtifactPanel: true,
        splitDrawer: true,
        scopeKey: 'approval:run-pending:gate:1',
        runId: 'run-pending',
      })
      expect(pending.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(false)
      expect(readArtifactPanelOpen()).toBe(false)
      expect(localStorage.length).toBe(0)

      await pending.wrapper.get('[data-testid="conversation-panel-toggle"]').trigger('click')
      expect(pending.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(true)
      expect(pending.wrapper.get('[data-testid="conversation-artifact-panel"]').attributes('data-drawer-mode')).toBe('artifacts')
      expect(readArtifactPanelOpen()).toBe(true)
      expect(openArtifact).not.toHaveBeenCalled()
      expect(localStorage.length).toBe(0)

      const epoch = ensureConversationDrawer('run-pending')!.resetEpoch
      await pending.wrapper.setProps({ scopeKey: 'inbox:run-pending:node:2' })
      expect(pending.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(true)
      expect(pending.wrapper.get('[data-testid="conversation-panel-toggle"]').attributes('aria-pressed')).toBe('true')
      expect(readArtifactPanelOpen()).toBe(true)
      expect(openArtifact).not.toHaveBeenCalled()
      expect(ensureConversationDrawer('run-pending')!.resetEpoch).toBe(epoch + 1)

      pending.wrapper.unmount()
      const recent = mountShell({
        rememberArtifactPanel: true,
        splitDrawer: true,
        scopeKey: 'run-review:run-recent:node:1',
        runId: 'run-recent',
      })
      expect(recent.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(true)
      expect(recent.wrapper.get('[data-testid="conversation-panel-toggle"]').attributes('aria-pressed')).toBe('true')
      expect(openArtifact).not.toHaveBeenCalled()

      await recent.wrapper.get('[data-testid="conversation-panel-toggle"]').trigger('click')
      expect(readArtifactPanelOpen()).toBe(false)
      expect(recent.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(false)

      await recent.wrapper.setProps({ scopeKey: 'run-clarify:run-recent:node:2' })
      expect(recent.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(false)
      expect(recent.wrapper.get('[data-testid="conversation-panel-toggle"]').attributes('aria-pressed')).toBe('false')
      expect(readArtifactPanelOpen()).toBe(false)

      recent.wrapper.unmount()
      const closed = mountShell({
        rememberArtifactPanel: true,
        scopeKey: 'approval:run-pending:gate:3',
        runId: 'run-pending',
      })
      expect(closed.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(false)
      await closed.wrapper.get('[data-testid="conversation-panel-toggle"]').trigger('click')
      expect(readArtifactPanelOpen()).toBe(true)
      closed.wrapper.unmount()

      const swapped = mountShell({
        rememberArtifactPanel: true,
        splitDrawer: true,
        scopeKey: 'run-review:run-recent:node:3',
        runId: 'run-recent',
      })
      expect(swapped.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(true)
      await swapped.wrapper.setProps({ scopeKey: 'inbox:run-recent:other:1' })
      expect(swapped.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(true)
      expect(openArtifact).not.toHaveBeenCalled()
      expect(openPreview).not.toHaveBeenCalled()
      expect(localStorage.length).toBe(0)
    } finally {
      unregisterPending()
      unregisterRecent()
    }
  })

  it('opens a cited file once and does not open it again when the scope changes', async () => {
    const openArtifact = vi.fn()
    const unregister = registerStageLinks('run-1', { hasArtifact: () => true, openArtifact, canOpenPreview: () => false, openPreview: vi.fn() })
    try {
      const host = mountShell({ rememberArtifactPanel: true, splitDrawer: true })
      await host.openArtifact({ name: 'review.json', runId: 'run-1' })
      expect(openArtifact).toHaveBeenCalledTimes(1)
      expect(openArtifact).toHaveBeenCalledWith('review.json')
      expect(readArtifactPanelOpen()).toBe(true)
      await host.wrapper.setProps({ scopeKey: 'run:node:2' })
      expect(host.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(true)
      expect(openArtifact).toHaveBeenCalledTimes(1)
      expect(readArtifactPanelOpen()).toBe(true)
    } finally { unregister() }
  })

  it('keeps chat-only and public shells from rewriting the session switch', async () => {
    writeArtifactPanelOpen(true)
    const embedded = mountShell({ chatOnly: true, rememberArtifactPanel: true, scopeKey: 'embedded-review:1' })
    expect(embedded.wrapper.find('[data-testid="conversation-panel-toggle"]').exists()).toBe(false)
    expect(embedded.wrapper.find('[data-testid="review-shell-stage"]').exists()).toBe(false)
    expect(readArtifactPanelOpen()).toBe(true)
    await embedded.wrapper.setProps({ scopeKey: 'embedded-review:2' })
    expect(readArtifactPanelOpen()).toBe(true)

    resetConversationDrawers()
    const pub = mountShell({ rememberArtifactPanel: false, scopeKey: 'public-review:1', runId: 'public-share' })
    expect(pub.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(false)
    await pub.wrapper.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    expect(pub.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(true)
    expect(readArtifactPanelOpen()).toBe(false)
    writeArtifactPanelOpen(true)
    await pub.wrapper.setProps({ scopeKey: 'public-review:2' })
    expect(pub.wrapper.get('[data-testid="stage-slot"]').isVisible()).toBe(false)
    expect(readArtifactPanelOpen()).toBe(true)
  })
})
