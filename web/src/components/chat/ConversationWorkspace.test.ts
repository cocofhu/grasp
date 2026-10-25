// @vitest-environment happy-dom
import { h } from 'vue'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { notifyRunArtifactEdit } from '@/lib/chat/artifactUnread'
import { CONVERSATION_PANEL_SHOWN_EVENT } from '@/lib/chat/conversationArtifacts'
import ConversationWorkspace from './ConversationWorkspace.vue'
import pages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'

let wrapper: VueWrapper | undefined
let resizeObserver: ResizeObserverCallback
const i18n = () => createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })
function mountWorkspace(open = false, extra: Record<string, unknown> = {}) {
  let queued: boolean | undefined
  wrapper = mount(ConversationWorkspace, {
    attachTo: document.body,
    props: { scopeKey: 'run-a/node-a', open, 'onUpdate:open': (value: boolean) => { if (wrapper) void wrapper.setProps({ open: value }); else queued = value }, ...extra },
    slots: { default: '<textarea aria-label="消息">未发送的问题</textarea>', panel: '<input aria-label="产物内容" value="draft" />' },
    global: { plugins: [i18n()] },
  })
  if (queued !== undefined) void wrapper.setProps({ open: queued })
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

  it('places optional navigation before the clipped title and keeps narrow resource controls functional', async () => {
    const navigate = vi.fn()
    const longName = '需要在窄屏上省略但仍保留完整提示的运行标题'.repeat(6)
    wrapper = mount(ConversationWorkspace, {
      attachTo: document.body,
      props: { scopeKey: 'mobile-navigation', runName: longName, splitModes: true },
      slots: {
        leading: () => h('button', { type: 'button', 'aria-label': '返回列表', 'data-test': 'conversation-back', onClick: navigate }, '返回'),
        default: '<textarea aria-label="消息">保留草稿</textarea>',
        panel: '<div>预览</div>',
      },
      global: { plugins: [i18n()] },
    })
    const w = wrapper
    resizeObserver([{ contentRect: { width: 390 } } as ResizeObserverEntry], {} as ResizeObserver)
    await flushPromises()
    expect(w.classes()).toContain('is-compact')
    const toolbar = w.get('.conversation-toolbar')
    expect(toolbar.element.firstElementChild).toBe(w.get('[data-test="conversation-back"]').element)
    expect(w.get('[data-testid="conversation-title"]').classes()).toContain('truncate')
    expect(w.get('[data-testid="conversation-title"]').attributes('title')).toBe(longName)
    await w.get('[data-testid="conversation-resource-toggle"]').trigger('click')
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('data-drawer-mode')).toBe('resources')
    await w.get('[data-testid="conversation-panel-close"]').trigger('click')
    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('data-drawer-mode')).toBe('artifacts')
    await w.get('[data-testid="conversation-panel-close"]').trigger('click')
    expect((w.get('textarea').element as HTMLTextAreaElement).value).toBe('保留草稿')
    await w.get('[data-test="conversation-back"]').trigger('click')
    expect(navigate).toHaveBeenCalledOnce()
  })

  it('notifies mounted previews once when the artifact drawer opens', async () => {
    let shown = 0
    const onShow = () => { shown += 1 }
    window.addEventListener(CONVERSATION_PANEL_SHOWN_EVENT, onShow)
    const w = mountWorkspace()
    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    await flushPromises()
    expect(shown).toBe(1)
    await w.get('[data-testid="conversation-panel-close"]').trigger('click')
    await flushPromises()
    expect(shown).toBe(1)
    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    await flushPromises()
    expect(shown).toBe(2)
    window.removeEventListener(CONVERSATION_PANEL_SHOWN_EVENT, onShow)
  })

  it('supports keyboard panel resizing and persists the preference', async () => {
    const w = mountWorkspace(true)
    resizeObserver([{ contentRect: { width: 1200 } } as ResizeObserverEntry], {} as ResizeObserver)
    await flushPromises()
    const sash = w.get('[role="separator"]')
    expect(sash.attributes('aria-valuemin')).toBe('0')
    expect(sash.attributes('aria-valuemax')).toBe('1196')
    const before = Number(sash.attributes('aria-valuenow'))
    expect(before).toBe(440)
    await sash.trigger('keydown', { key: 'ArrowLeft' })
    expect(Number(sash.attributes('aria-valuenow'))).toBe(before + 24)
    expect(Number(localStorage.getItem('grasp.conversation.panelWidth'))).toBe(Number(sash.attributes('aria-valuenow')))
    await sash.trigger('keydown', { key: 'Home' })
    await flushPromises()
    expect(w.find('[data-testid="conversation-panel-sash"]').exists()).toBe(false)
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(localStorage.getItem('grasp.conversation.panelWidth')).toBe('440')
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
    expect(w.find('[data-testid="conversation-panel-sash"]').exists()).toBe(false)
  })

  it('closes the old preview when switching conversation scope', async () => {
    const w = mountWorkspace(true)
    await flushPromises()
    await w.setProps({ scopeKey: 'run-b/node-b' })
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
  })

  it('keeps the panel open across scope changes when the parent retains it', async () => {
    const closed = mountWorkspace(false, { retainOpen: true })
    notifyRunArtifactEdit()
    await flushPromises()
    expect(closed.get('[data-testid="artifact-unread-badge"]').text()).toBe('1')
    await closed.setProps({ scopeKey: 'run-b/node-b' })
    await flushPromises()
    expect(closed.find('[data-testid="artifact-unread-badge"]').exists()).toBe(false)
    closed.unmount()

    const w = mountWorkspace(true, { retainOpen: true })
    await flushPromises()
    await w.setProps({ scopeKey: 'run-b/node-b' })
    await flushPromises()
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(true)
    expect(w.get('[data-testid="conversation-panel-toggle"]').attributes('aria-expanded')).toBe('true')
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

describe('ConversationWorkspace panel sash', () => {
  function firePointer(el: Element, type: 'pointerdown' | 'pointermove' | 'pointerup', clientX: number) {
    el.dispatchEvent(new PointerEvent(type, { bubbles: true, cancelable: true, button: 0, clientX, pointerId: 1 }))
  }
  async function setCanvas(width: number) {
    resizeObserver([{ contentRect: { width } } as ResizeObserverEntry], {} as ResizeObserver)
    await flushPromises()
  }
  async function dragTo(w: VueWrapper, target: number) {
    const sash = w.get('[data-testid="conversation-panel-sash"]')
    const current = Number(sash.attributes('aria-valuenow'))
    firePointer(sash.element, 'pointerdown', 0)
    firePointer(sash.element, 'pointermove', current - target)
    await flushPromises()
    if (w.find('[data-testid="conversation-panel-sash"]').exists()) firePointer(sash.element, 'pointerup', current - target)
    await flushPromises()
  }

  it('covers the chat when dragged to the left edge and hides the panel at the right edge', async () => {
    const w = mountWorkspace(true)
    await w.get('textarea').setValue('保留我的草稿')
    await setCanvas(1200)
    const sash = w.get('[data-testid="conversation-panel-sash"]')
    expect(sash.attributes('aria-valuemin')).toBe('0')
    expect(sash.attributes('aria-valuemax')).toBe('1196')

    await dragTo(w, 1179)
    expect(w.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('1179')
    expect(w.get('[data-testid="conversation-main"]').attributes('inert')).toBeUndefined()

    await dragTo(w, 1180)
    const covered = w.get('[data-testid="conversation-panel-sash"]')
    expect(covered.attributes('aria-valuenow')).toBe('1196')
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('style')).toContain('width: 1196px')
    expect(w.classes()).toContain('is-covered')
    expect(w.get('[data-testid="conversation-main"]').attributes('inert')).toBeDefined()
    expect(localStorage.getItem('grasp.conversation.panelWidth')).toBe('cover')

    await dragTo(w, 700)
    expect(w.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('700')
    expect(w.get('[data-testid="conversation-main"]').attributes('inert')).toBeUndefined()
    expect(localStorage.getItem('grasp.conversation.panelWidth')).toBe('700')

    await dragTo(w, 17)
    expect(w.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('17')
    await dragTo(w, 0)
    expect(w.find('[data-testid="conversation-panel-sash"]').exists()).toBe(false)
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(localStorage.getItem('grasp.conversation.panelWidth')).toBe('440')
    expect((w.get('textarea').element as HTMLTextAreaElement).value).toBe('保留我的草稿')

    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    await flushPromises()
    const reopened = w.get('[data-testid="conversation-panel-sash"]')
    expect(reopened.attributes('aria-valuenow')).toBe('440')
    expect(reopened.attributes('aria-valuemax')).toBe('1196')
    expect(w.classes()).not.toContain('is-covered')
    expect((w.get('textarea').element as HTMLTextAreaElement).value).toBe('保留我的草稿')

    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="conversation-panel-sash"]').exists()).toBe(false)
    expect(w.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
  })

  it('keeps a close-button width for the parent and still opens the icon at 440', async () => {
    const w = mountWorkspace(true)
    await setCanvas(1200)
    await dragTo(w, 700)
    await w.get('[data-testid="conversation-panel-close"]').trigger('click')
    await flushPromises()
    expect(localStorage.getItem('grasp.conversation.panelWidth')).toBe('700')
    await w.setProps({ open: true })
    await flushPromises()
    expect(w.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('700')
    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('440')
    expect(localStorage.getItem('grasp.conversation.panelWidth')).toBe('440')
  })

  it('uses End, Home, arrows, and Enter on the open sash', async () => {
    const w = mountWorkspace(true)
    await setCanvas(1200)
    const sash = () => w.get('[data-testid="conversation-panel-sash"]')
    await sash().trigger('keydown', { key: 'End' })
    expect(sash().attributes('aria-valuenow')).toBe('1196')
    expect(w.get('[data-testid="conversation-main"]').attributes('inert')).toBeDefined()
    await sash().trigger('keydown', { key: 'ArrowRight' })
    expect(sash().attributes('aria-valuenow')).toBe('1172')
    expect(w.get('[data-testid="conversation-main"]').attributes('inert')).toBeUndefined()
    await sash().trigger('keydown', { key: 'Enter' })
    expect(sash().attributes('aria-valuenow')).toBe('440')
    await dragTo(w, 41)
    await sash().trigger('keydown', { key: 'ArrowRight' })
    expect(sash().attributes('aria-valuenow')).toBe('17')
    await sash().trigger('keydown', { key: 'ArrowLeft' })
    expect(sash().attributes('aria-valuenow')).toBe('41')
    await dragTo(w, 1179)
    await sash().trigger('keydown', { key: 'ArrowLeft' })
    expect(sash().attributes('aria-valuenow')).toBe('1196')
    await sash().trigger('keydown', { key: 'Home' })
    await flushPromises()
    expect(w.find('[data-testid="conversation-panel-sash"]').exists()).toBe(false)
  })

  it('keeps full cover across a wider canvas and a narrow drawer round-trip', async () => {
    const w = mountWorkspace(true)
    await setCanvas(1200)
    await w.get('[data-testid="conversation-panel-sash"]').trigger('keydown', { key: 'End' })
    await setCanvas(1500)
    expect(w.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('1496')
    expect(w.get('[data-testid="conversation-main"]').attributes('inert')).toBeDefined()
    await setCanvas(600)
    expect(w.find('[data-testid="conversation-panel-sash"]').exists()).toBe(false)
    expect(w.get('[role="dialog"]').attributes('aria-modal')).toBe('true')
    await setCanvas(1200)
    expect(w.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('1196')
    expect(w.get('[data-testid="conversation-main"]').attributes('inert')).toBeDefined()
    expect(localStorage.getItem('grasp.conversation.panelWidth')).toBe('cover')
  })

  it('restores cover after refresh and stays closed after the panel was hidden', async () => {
    localStorage.setItem('grasp.conversation.panelWidth', 'cover')
    const covered = mountWorkspace(false)
    await setCanvas(1200)
    expect(covered.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(true)
    expect(covered.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('1196')
    expect(covered.get('[data-testid="conversation-main"]').attributes('inert')).toBeDefined()
    covered.unmount()

    localStorage.setItem('grasp.conversation.panelWidth', '440')
    const hidden = mountWorkspace(false)
    await setCanvas(1200)
    expect(hidden.get('[data-testid="conversation-artifact-panel"]').isVisible()).toBe(false)
    expect(hidden.find('[data-testid="conversation-panel-sash"]').exists()).toBe(false)
  })

  it('reopens a hidden split drawer at 440 from the artifact icon', async () => {
    const state = { open: false, mode: null as 'resources' | 'artifacts' | null }
    wrapper = mount(ConversationWorkspace, {
      attachTo: document.body,
      props: {
        scopeKey: 'run-a',
        splitModes: true,
        open: state.open,
        mode: state.mode,
        'onUpdate:open': (value: boolean) => { state.open = value; void wrapper?.setProps({ open: value }) },
        'onUpdate:mode': (value: 'resources' | 'artifacts' | null) => { state.mode = value; void wrapper?.setProps({ mode: value }) },
      },
      slots: { default: '<textarea aria-label="消息">未发送的问题</textarea>', panel: '<input aria-label="产物内容" value="draft" />' },
      global: { plugins: [i18n()] },
    })
    const w = wrapper
    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    await flushPromises()
    await setCanvas(1200)
    expect(w.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('440')
    await dragTo(w, 1196)
    expect(w.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('1196')
    await dragTo(w, -20)
    expect(w.find('[data-testid="conversation-panel-sash"]').exists()).toBe(false)
    expect(w.get('[data-testid="conversation-panel-toggle"]').attributes('aria-pressed')).toBe('false')
    await w.get('[data-testid="conversation-panel-toggle"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="conversation-panel-sash"]').attributes('aria-valuenow')).toBe('440')
    expect(w.classes()).not.toContain('is-covered')
    expect(w.get('[data-testid="conversation-panel-toggle"]').attributes('aria-pressed')).toBe('true')
  })

  it('does not render a sash on a 600px canvas', async () => {
    const w = mountWorkspace(true)
    await setCanvas(600)
    expect(w.classes()).toContain('is-compact')
    expect(w.find('[data-testid="conversation-panel-sash"]').exists()).toBe(false)
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('role')).toBe('dialog')
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
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('aria-label')).toBe('产物')
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

  it('uses the English resource tip and no duplicate drawer heading row', async () => {
    const { wrapper: w } = mountSplit('en')
    const resource = w.get('[data-testid="conversation-resource-toggle"]')
    expect(resource.attributes('title')).toBe('Resource')
    expect(resource.attributes('aria-label')).toBe('Open resources')
    expect(w.get('[data-testid="conversation-panel-toggle"]').attributes('title')).toBe('Artifacts')
    await resource.trigger('click')
    await flushPromises()
    expect(resource.attributes('aria-label')).toBe('Close resources')
    expect(w.get('[data-testid="conversation-artifact-panel"]').attributes('aria-label')).toBe('Resources')
    // The toolbar toggles name and close the split drawer, so desktop has no heading row.
    expect(w.find('[data-testid="conversation-panel-close"]').exists()).toBe(false)
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

describe('ConversationWorkspace toolbar run name', () => {
  const longName = '优化这里的段间距'.repeat(8)

  it('keeps the fixed label when no run name is passed', () => {
    const w = mountWorkspace()
    const title = w.get('[data-testid="conversation-title"]')
    expect(title.text()).toBe('对话')
    expect(title.attributes('title')).toBeUndefined()
    expect(w.find('[data-testid="conversation-title-skeleton"]').exists()).toBe(false)
    expect(w.get('.conversation-heading').get('[data-icon="chat"]').exists()).toBe(true)
    expect(w.get('[data-testid="conversation-panel-toggle"]').text()).toContain('产物')
  })

  it('shows the run name on one line and puts the full name on title', async () => {
    const w = mountWorkspace(false, { runName: longName })
    const title = w.get('[data-testid="conversation-title"]')
    expect(title.text()).toBe(longName)
    expect(title.attributes('title')).toBe(longName)
    expect(title.classes()).toContain('truncate')
    expect(w.get('.conversation-heading').text()).not.toContain('对话')
    expect(w.find('[data-testid="conversation-title-skeleton"]').exists()).toBe(false)
    await w.setProps({ runName: '丙' })
    expect(w.get('[data-testid="conversation-title"]').text()).toBe('丙')
    expect(w.get('[data-testid="conversation-title"]').attributes('title')).toBe('丙')
  })

  it('shows the sidebar placeholder when the run name is blank', async () => {
    const w = mountWorkspace(false, { runName: '' })
    expect(w.find('[data-testid="conversation-title"]').exists()).toBe(false)
    expect(w.get('[data-testid="conversation-title-skeleton"]').find('span').exists()).toBe(true)
    expect(w.get('.conversation-heading').text()).not.toContain('对话')
    expect(w.get('.conversation-heading').text()).not.toContain('Conversation')
    expect(w.get('.conversation-heading').get('[data-icon="chat"]').exists()).toBe(true)
    await w.setProps({ runName: '把登录做清楚' })
    expect(w.find('[data-testid="conversation-title-skeleton"]').exists()).toBe(false)
    expect(w.get('[data-testid="conversation-title"]').text()).toBe('把登录做清楚')
  })

  it('shows the run name in English instead of Conversation', () => {
    wrapper = mount(ConversationWorkspace, {
      props: { scopeKey: 'run-a', runName: '优化这里的段间距' },
      global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: enPages } })] },
    })
    expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe('优化这里的段间距')
    expect(wrapper.get('.conversation-heading').text()).not.toContain('Conversation')
  })
})
