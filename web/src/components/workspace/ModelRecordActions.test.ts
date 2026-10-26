// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ModelRecordActions from './ModelRecordActions.vue'

const wrappers: VueWrapper[] = []
function setup(active = true, recordId = 'service') {
  const wrapper = mount(ModelRecordActions, {
    attachTo: document.body,
    props: { name: 'Team model service', active, prefix: 'model-connection', recordId },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} } })],
      stubs: { Icon: true },
    },
  })
  vi.spyOn(wrapper.get(`[data-testid="model-connection-more-${recordId}"]`).element, 'getBoundingClientRect').mockReturnValue(new DOMRect(180, 20, 28, 28))
  wrappers.push(wrapper)
  return wrapper
}

function deleteAction() {
  const button = document.querySelector<HTMLButtonElement>('[data-testid="model-connection-delete-service"]')
  if (!button) throw new Error('Missing model record delete action')
  return button
}

async function pressKey(key: string) {
  document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
  await flushPromises()
}

afterEach(() => {
  for (const wrapper of wrappers.splice(0)) if (wrapper.exists()) wrapper.unmount()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('ModelRecordActions', () => {
  it('offers accessible editing and an enabled switch while deletion stays in the menu', async () => {
    const wrapper = setup()
    expect(document.querySelector('[role="menu"]')).toBeNull()
    const toggle = wrapper.get('[data-testid="model-connection-toggle-service"]')
    expect(toggle.attributes('role')).toBe('switch')
    expect(toggle.attributes('aria-checked')).toBe('true')
    expect(document.querySelector('[data-testid="model-connection-delete-service"]')).toBeNull()
    const edit = wrapper.get('[data-testid="model-connection-edit-service"]')
    expect(edit.attributes('aria-label')).toBe('Edit')
    await edit.trigger('click')
    expect(wrapper.emitted('edit')).toEqual([[]])
    expect(wrapper.emitted('toggle')).toBeUndefined()
    expect(wrapper.emitted('remove')).toBeUndefined()
  })

  it('supports keyboard navigation and restores focus when Escape closes the teleported menu', async () => {
    const visibilityAtFocus: string[] = []
    const originalFocus = HTMLElement.prototype.focus
    HTMLElement.prototype.focus = function (this: HTMLElement, ...args: Parameters<HTMLElement['focus']>) {
      if (this.getAttribute('role') === 'menuitem') visibilityAtFocus.push(this.style.visibility)
      return originalFocus.apply(this, args)
    }
    try {
      const wrapper = setup()
      const trigger = wrapper.get('[data-testid="model-connection-more-service"]')
      expect(trigger.attributes('aria-label')).toContain('Team model service')
      expect(trigger.attributes('aria-haspopup')).toBe('menu')
      expect(trigger.attributes('aria-expanded')).toBe('false')
      await trigger.trigger('keydown', { key: 'ArrowDown' })
      await flushPromises()
      expect(visibilityAtFocus.length).toBeGreaterThan(0)
      expect(visibilityAtFocus.every((value) => value !== 'hidden')).toBe(true)
      const menu = document.querySelector<HTMLElement>('[role="menu"]')!
      expect(menu.parentElement).toBe(document.body)
      expect(menu.getAttribute('aria-label')).toBe('Team model service')
      expect(trigger.attributes('aria-controls')).toBe(menu.id)
      expect(trigger.attributes('aria-expanded')).toBe('true')
      expect(menu.querySelectorAll('[role="menuitem"]')).toHaveLength(1)
      expect(document.activeElement).toBe(deleteAction())
      await pressKey('Escape')
      expect(document.querySelector('[role="menu"]')).toBeNull()
      expect(document.activeElement).toBe(trigger.element)
      expect(trigger.attributes('aria-expanded')).toBe('false')
      await trigger.trigger('keydown', { key: 'ArrowUp' })
      await flushPromises()
      expect(document.activeElement).toBe(deleteAction())
      const tab = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
      document.activeElement!.dispatchEvent(tab)
      await flushPromises()
      expect(tab.defaultPrevented).toBe(false)
      expect(document.querySelector('[role="menu"]')).toBeNull()
      expect(document.activeElement).toBe(trigger.element)
    } finally {
      HTMLElement.prototype.focus = originalFocus
    }
  })

  it('closes on an outside press and leaves presses inside the menu available for actions', async () => {
    const wrapper = setup()
    const trigger = wrapper.get('[data-testid="model-connection-more-service"]')
    await trigger.trigger('click')
    await flushPromises()
    deleteAction().dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).not.toBeNull()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).toBeNull()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(wrapper.emitted('toggle')).toBeUndefined()
    expect(wrapper.emitted('remove')).toBeUndefined()
  })

  it('dispatches toggle and remove separately and leaves deletion confirmation to its parent', async () => {
    const confirm = vi.spyOn(window, 'confirm')
    const wrapper = setup()
    const trigger = wrapper.get('[data-testid="model-connection-more-service"]')
    await wrapper.get('[data-testid="model-connection-toggle-service"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('toggle')).toEqual([[]])
    expect(document.querySelector('[role="menu"]')).toBeNull()
    await trigger.trigger('click')
    await flushPromises()
    expect(deleteAction().getAttribute('role')).toBe('menuitem')
    deleteAction().click()
    await flushPromises()
    expect(wrapper.emitted('remove')).toEqual([[]])
    expect(document.querySelector('[role="menu"]')).toBeNull()
    expect(confirm).not.toHaveBeenCalled()
  })

  it('shows a disabled record as unchecked and dispatches its switch change', async () => {
    const wrapper = setup(false)
    const toggle = wrapper.get('[data-testid="model-connection-toggle-service"]')
    expect(toggle.attributes('role')).toBe('switch')
    expect(toggle.attributes('aria-checked')).toBe('false')
    await toggle.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('toggle')).toEqual([[]])
  })

  it('can show only the more menu so an API account keeps its own fill and clear controls', async () => {
    const wrapper = mount(ModelRecordActions, {
      attachTo: document.body,
      props: { name: '1', prefix: 'api-account', recordId: 'empty-api', showEdit: false, showToggle: false },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} } })],
        stubs: { Icon: true },
      },
    })
    wrappers.push(wrapper)
    expect(wrapper.find('[data-testid="api-account-edit-empty-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="api-account-toggle-empty-api"]').exists()).toBe(false)
    const trigger = wrapper.get('[data-testid="api-account-more-empty-api"]')
    expect(trigger.attributes('aria-label')).toContain('1')
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(180, 20, 28, 28))
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const remove = document.querySelector<HTMLButtonElement>('[data-testid="api-account-delete-empty-api"]')!
    expect(remove.getAttribute('aria-label')).toBe('Delete · 1')
    expect(document.activeElement).toBe(remove)
    await pressKey('Escape')
    expect(document.querySelector('[role="menu"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
  })

  it('keeps one record menu open and dismisses it when the list moves or the record unmounts', async () => {
    const first = setup()
    const second = setup(true, 'second')
    const firstTrigger = first.get('[data-testid="model-connection-more-service"]')
    const secondTrigger = second.get('[data-testid="model-connection-more-second"]')
    await firstTrigger.trigger('click')
    await flushPromises()
    await secondTrigger.trigger('click')
    await flushPromises()
    expect(document.querySelectorAll('[role="menu"]')).toHaveLength(1)
    expect(firstTrigger.attributes('aria-expanded')).toBe('false')
    expect(secondTrigger.attributes('aria-expanded')).toBe('true')
    window.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).toBeNull()
    await secondTrigger.trigger('click')
    await flushPromises()
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).toBeNull()
    await secondTrigger.trigger('click')
    await flushPromises()
    second.unmount()
    expect(document.querySelector('[role="menu"]')).toBeNull()
    await firstTrigger.trigger('click')
    await flushPromises()
    expect(firstTrigger.attributes('aria-expanded')).toBe('true')
  })
})
