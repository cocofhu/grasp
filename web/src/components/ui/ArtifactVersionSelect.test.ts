// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ArtifactVersionSelect from './ArtifactVersionSelect.vue'
import type { ArtifactVersionChoice } from '@/lib/run/reactArtifactPreview'

const choices: ArtifactVersionChoice[] = [
  { index: 1, revision: 1, latest: false, available: true },
  { index: 2, revision: 2, latest: true, available: true },
]

function manyChoices(n: number): ArtifactVersionChoice[] {
  return Array.from({ length: n }, (_, i) => ({
    index: i + 1,
    revision: i + 1,
    latest: i === n - 1,
    available: true,
  }))
}

function mountSelect(
  extra: Record<string, unknown> = {},
  rect: Partial<DOMRect> = { top: 400, bottom: 420, left: 200, right: 280, width: 80, height: 20 },
) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': {} } })
  const wrapper = mount(ArtifactVersionSelect, {
    props: {
      choices,
      selectedIndex: 2,
      currentLabel: 'v2 · 最新',
      menuAriaLabel: '版本',
      chipTestId: 'react-artifact-version-chip',
      buttonTestId: 'react-artifact-version-chip-btn-page.html',
      menuTestId: 'react-artifact-version-menu',
      optionTestIdPrefix: 'react-artifact-version-option-v',
      labelFor: (c: ArtifactVersionChoice) => (c.latest ? `v${c.index} · 最新` : `v${c.index}`),
      ...extra,
    },
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { Teleport: false } },
  })
  const btn = wrapper.get('[data-testid="react-artifact-version-chip-btn-page.html"]').element as HTMLElement
  vi.spyOn(btn, 'getBoundingClientRect').mockReturnValue({
    x: rect.left ?? 200,
    y: rect.top ?? 400,
    top: rect.top ?? 400,
    bottom: rect.bottom ?? 420,
    left: rect.left ?? 200,
    right: rect.right ?? 280,
    width: rect.width ?? 80,
    height: rect.height ?? 20,
    toJSON() {
      return {}
    },
  })
  return wrapper
}

describe('ArtifactVersionSelect', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('teleports the menu onto body instead of the trigger wrapper', async () => {
    const wrapper = mountSelect()
    await wrapper.get('[data-testid="react-artifact-version-chip-btn-page.html"]').trigger('click')
    await flushPromises()
    const menu = document.querySelector('[data-testid="react-artifact-version-menu"]') as HTMLElement
    expect(menu).toBeTruthy()
    expect(menu.parentElement).toBe(document.body)
    expect(wrapper.find('[data-testid="react-artifact-version-menu"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('scrolls when there are many versions', async () => {
    const wrapper = mountSelect({ choices: manyChoices(20), currentLabel: 'v20 · 最新', selectedIndex: 20 })
    await wrapper.get('[data-testid="react-artifact-version-chip-btn-page.html"]').trigger('click')
    await flushPromises()
    const menu = document.querySelector('[data-testid="react-artifact-version-menu"]') as HTMLElement
    expect(menu).toBeTruthy()
    expect(menu.style.maxHeight).toBeTruthy()
    expect(menu.style.overflowY).toBe('auto')
    wrapper.unmount()
  })

  it('opens downward when the trigger is near the top of the viewport', async () => {
    const wrapper = mountSelect({}, { top: 12, bottom: 32, left: 200, right: 280, width: 80, height: 20 })
    await wrapper.get('[data-testid="react-artifact-version-chip-btn-page.html"]').trigger('click')
    await flushPromises()
    const menu = document.querySelector('[data-testid="react-artifact-version-menu"]') as HTMLElement
    expect(menu.getAttribute('data-placement')).toBe('below')
    wrapper.unmount()
  })
})
