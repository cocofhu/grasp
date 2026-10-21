// @vitest-environment happy-dom
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { notifyRunArtifactEdit } from '@/lib/chat/artifactUnread'
import ConversationWorkspace from './ConversationWorkspace.vue'
import pages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'

let wrapper: VueWrapper | undefined
let resizeObserver: ResizeObserverCallback
const i18n = () => createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })
function mountWorkspace(open = false, extra: Record<string, unknown> = {}) {
  wrapper = mount(ConversationWorkspace, {
    attachTo: document.body,
    props: { scopeKey: 'run-a/node-a', open, 'onUpdate:open': value => { void wrapper?.setProps({ open: value }) }, ...extra },
    slots: { default: '<textarea aria-label="消息">未发送的问题</textarea>', panel: '<input aria-label="产物内容" value="draft" />' },
    global: { plugins: [i18n()] },
  })
  return wrapper
}
beforeEach(() => {
  localStorage.clear()
  vi.stubGlobal('ResizeObserver', class { constructor(callback: ResizeObserverCallback) { resizeObserver = callback } observe() {} disconnect() {} })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.unstubAllGlobals() })

describe('ConversationWorkspace', () => {
  it('opens artifacts to the right without remounting the conversation or preview', async () => {
    const w = mountWorkspace()
    const chat = w.get('textarea').element
    const preview = w.get('input').element
    await w.get('textarea').setValue('保留我的草稿')
    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(true)
    await w.get('[data-testid="conversation-panel-close"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(w.get('textarea').element).toBe(chat)
    expect(w.get('input').element).toBe(preview)
    expect((chat as HTMLTextAreaElement).value).toBe('保留我的草稿')
  })

  it('supports keyboard panel resizing and persists the preference', async () => {
    const w = mountWorkspace(true)
    await flushPromises()
    const sash = w.get('[role="separator"]')
    const before = Number(sash.attributes('aria-valuenow'))
    await sash.trigger('keydown', { key: 'ArrowLeft' })
    expect(Number(sash.attributes('aria-valuenow'))).toBeGreaterThan(before)
    expect(Number(localStorage.getItem('grasp.conversation.panelWidth'))).toBe(Number(sash.attributes('aria-valuenow')))
    await sash.trigger('keydown', { key: 'Home' })
    expect(sash.attributes('aria-valuenow')).toBe('280')
  })

  it('uses a dismissible modal preview on a narrow canvas and returns focus', async () => {
    const w = mountWorkspace()
    resizeObserver([{ contentRect: { width: 600 } } as ResizeObserverEntry], {} as ResizeObserver)
    await flushPromises()
    const trigger = w.get('[data-testid="conversation-panel-toggle"]')
    ;(trigger.element as HTMLButtonElement).focus()
    await trigger.trigger('click')
    await flushPromises()
    const dialog = w.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(w.get('[data-testid="conversation-main"]').attributes('inert')).toBeDefined()
    await dialog.trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    expect(w.get('[data-testid="conversation-main"]').attributes('aria-hidden')).toBeUndefined()
  })

  it('closes the old preview when switching conversation scope', async () => {
    const w = mountWorkspace(true)
    await flushPromises()
    await w.setProps({ scopeKey: 'run-b/node-b' })
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
  })

  it('keeps a single labeled artifact button unless the sandbox drawer is split', () => {
    const w = mountWorkspace()
    expect(w.find('[data-testid="conversation-resource-toggle"]').exists()).toBe(false)
    expect(w.get('[data-testid="conversation-panel-toggle"]').text()).toContain('产物')
  })

  it('puts a counted badge on the artifact icon and removes the node after viewing', async () => {
    const w = mountWorkspace()
    const button = () => w.get('[data-testid="conversation-panel-toggle"]')
    expect(w.find('[data-testid="artifact-unread-badge"]').exists()).toBe(false)
    expect(button().attributes('aria-label')).toBe('打开产物')

    notifyRunArtifactEdit()
    await flushPromises()
    const first = button().get('[data-testid="artifact-unread-badge"]')
    expect(first.text()).toBe('1')
    expect(first.attributes('aria-hidden')).toBe('true')
    expect(first.attributes('hidden')).toBeUndefined()
    expect(button().attributes('aria-label')).toBe('打开产物，1 项未查看')
    const glyph = button().get('[data-testid="artifact-unread-glyph"]')
    expect(glyph.element.contains(first.element)).toBe(true)
    expect(glyph.get('[data-icon="chat-artifacts"]').exists()).toBe(true)
    expect(glyph.text()).not.toContain('产物')
    expect(button().text()).toContain('产物')

    notifyRunArtifactEdit()
    await flushPromises()
    const second = button().get('[data-testid="artifact-unread-badge"]')
    expect(second.text()).toBe('2')
    expect(second.element).not.toBe(first.element)
    expect(button().attributes('aria-label')).toBe('打开产物，2 项未查看')

    await button().trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="artifact-unread-badge"]').exists()).toBe(false)
    expect(button().attributes('aria-label')).toBe('关闭产物')
    expect(button().html()).not.toContain('artifact-unread-badge')

    notifyRunArtifactEdit()
    await flushPromises()
    expect(w.find('[data-testid="artifact-unread-badge"]').exists()).toBe(false)

    await button().trigger('click')
    await flushPromises()
    notifyRunArtifactEdit()
    await flushPromises()
    expect(button().get('[data-testid="artifact-unread-badge"]').text()).toBe('1')
  })

  it('clears the badge when the conversation scope changes and caps the label at 99+', async () => {
    const w = mountWorkspace()
    notifyRunArtifactEdit()
    await flushPromises()
    expect(w.get('[data-testid="artifact-unread-badge"]').text()).toBe('1')
    await w.setProps({ scopeKey: 'run-b/node-b' })
    await flushPromises()
    expect(w.find('[data-testid="artifact-unread-badge"]').exists()).toBe(false)
    expect(w.get('[data-testid="conversation-panel-toggle"]').attributes('aria-label')).toBe('打开产物')

    for (let i = 0; i < 99; i += 1) notifyRunArtifactEdit()
    await flushPromises()
    const capped = w.get('[data-testid="artifact-unread-badge"]')
    expect(capped.text()).toBe('99+')
    expect(w.get('[data-testid="conversation-panel-toggle"]').attributes('aria-label')).toBe('打开产物，99 项及以上未查看')
    const cappedNode = capped.element
    notifyRunArtifactEdit()
    await flushPromises()
    expect(w.get('[data-testid="artifact-unread-badge"]').element).toBe(cappedNode)
    expect(w.get('[data-testid="artifact-unread-badge"]').text()).toBe('99+')
  })

  it('does not keep a badge for a hidden toolbar', async () => {
    const w = mountWorkspace(false, { showToolbar: false })
    notifyRunArtifactEdit()
    notifyRunArtifactEdit()
    await flushPromises()
    expect(w.find('[data-testid="conversation-panel-toggle"]').exists()).toBe(false)
    expect(w.find('[data-testid="artifact-unread-badge"]').exists()).toBe(false)
    await w.setProps({ showToolbar: true })
    await flushPromises()
    expect(w.find('[data-testid="artifact-unread-badge"]').exists()).toBe(false)
  })

  it('pops the badge once and stays still when motion is reduced', () => {
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'ArtifactUnreadMark.vue'), 'utf8')
    expect(src).toMatch(/animation:\s*artifact-unread-pop 280ms ease/)
    expect(src).toMatch(/@keyframes artifact-unread-pop[\s\S]*opacity:\s*0[\s\S]*scale\(0\.55\)[\s\S]*opacity:\s*1[\s\S]*scale\(1\)/)
    expect(src).toMatch(/background:\s*rgb\(var\(--c-accent\)\)/)
    expect(src).toMatch(/box-shadow:\s*0 0 0 1\.5px rgb\(var\(--c-surface\)\)/)
    expect(src).toMatch(/color:\s*#fff/)
    expect(src).not.toMatch(/--c-err|bg-err/)
    expect(src).not.toMatch(/\shidden\b/)
    expect(src).toMatch(/prefers-reduced-motion:\s*reduce[\s\S]*\.artifact-unread-badge\s*\{[^}]*animation:\s*none/)
  })
})

describe('ConversationWorkspace split modes', () => {
  function mountSplit(locale: 'zh-CN' | 'en' = 'zh-CN') {
    const state = { open: false, mode: null as 'resources' | 'artifacts' | null, scopeKey: 'run-a' }
    const messages = locale === 'en' ? { en: enPages } : { 'zh-CN': pages }
    wrapper = mount(ConversationWorkspace, {
      attachTo: document.body,
      props: {
        scopeKey: state.scopeKey,
        splitModes: true,
        open: state.open,
        mode: state.mode,
        'onUpdate:open': (value: boolean) => { state.open = value; void wrapper?.setProps({ open: value }) },
        'onUpdate:mode': (value: 'resources' | 'artifacts' | null) => { state.mode = value; void wrapper?.setProps({ mode: value }) },
        'onUpdate:scopeKey': (value: string) => { state.scopeKey = value },
      },
      slots: { default: '<textarea aria-label="消息">未发送的问题</textarea>', panel: '<input aria-label="产物内容" value="draft" />' },
      global: { plugins: [createI18n({ legacy: false, locale, messages })] },
    })
    return { wrapper, state }
  }

  it('toggles one shared drawer from two icon buttons and restores neither after a new conversation', async () => {
    const { wrapper: w, state } = mountSplit()
    const resource = w.get('[data-testid="conversation-resource-toggle"]')
    const artifact = w.get('[data-testid="conversation-panel-toggle"]')
    expect(resource.text().trim()).toBe('')
    expect(artifact.text().trim()).toBe('')
    expect(resource.attributes('title')).toBe('资源')
    expect(artifact.attributes('title')).toBe('产物')
    expect(resource.attributes('aria-label')).toBe('打开资源')
    expect(artifact.attributes('aria-label')).toBe('打开产物')
    expect(resource.classes()).toContain('conversation-mode-toggle')
    expect(artifact.classes()).toContain('conversation-mode-toggle')
    await resource.trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(true)
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('data-drawer-mode')).toBe('resources')
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('aria-label')).toBe('资源')
    expect(resource.attributes('aria-pressed')).toBe('true')
    expect(artifact.attributes('aria-pressed')).toBe('false')
    expect(resource.attributes('aria-label')).toBe('关闭资源')
    await artifact.trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('data-drawer-mode')).toBe('artifacts')
    expect(w.get('[data-testid="conversation-artifact-panel"]').text()).toContain('产物')
    expect(artifact.attributes('aria-pressed')).toBe('true')
    expect(resource.attributes('aria-pressed')).toBe('false')
    await artifact.trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(artifact.attributes('aria-pressed')).toBe('false')
    expect(resource.attributes('aria-pressed')).toBe('false')
    await resource.trigger('click')
    await w.setProps({ scopeKey: 'run-b' })
    await flushPromises()
    expect(state.open).toBe(false)
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(resource.attributes('aria-pressed')).toBe('false')
    expect(artifact.attributes('aria-pressed')).toBe('false')
  })

  it('uses the English resource tip and a single Resources title', async () => {
    const { wrapper: w } = mountSplit('en')
    const resource = w.get('[data-testid="conversation-resource-toggle"]')
    expect(resource.attributes('title')).toBe('Resource')
    expect(resource.attributes('aria-label')).toBe('Open resources')
    expect(w.get('[data-testid="conversation-panel-toggle"]').attributes('title')).toBe('Artifacts')
    await resource.trigger('click')
    await flushPromises()
    expect(resource.attributes('aria-label')).toBe('Close resources')
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('aria-label')).toBe('Resources')
    const titles = w.get('[data-testid="conversation-artifact-panel"]').findAll('header span').map((n) => n.text())
    expect(titles.filter((text) => text === 'Resources')).toHaveLength(1)
  })

  it('covers the conversation with a full-screen drawer on a narrow canvas', async () => {
    const { wrapper: w } = mountSplit()
    resizeObserver([{ contentRect: { width: 600 } } as ResizeObserverEntry], {} as ResizeObserver)
    await flushPromises()
    const resource = w.get('[data-testid="conversation-resource-toggle"]')
    await resource.trigger('click')
    await flushPromises()
    const dialog = w.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    await dialog.trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(resource.attributes('aria-pressed')).toBe('false')
    expect(w.get('[data-testid="conversation-panel-toggle"]').attributes('aria-pressed')).toBe('false')
  })
})
