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

function stubWindowHeight(height: number) {
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: height })
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1024 })
}

function stubMenuHeight(menu: HTMLElement, height: number) {
  Object.defineProperty(menu, 'offsetHeight', { configurable: true, get: () => height })
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

async function openMenu(
  wrapper: ReturnType<typeof mountSelect>,
  menuHeight: number,
) {
  await wrapper.get('[data-testid="react-artifact-version-chip-btn-page.html"]').trigger('click')
  await flushPromises()
  const menu = document.querySelector('[data-testid="react-artifact-version-menu"]') as HTMLElement
  expect(menu).toBeTruthy()
  stubMenuHeight(menu, menuHeight)
  // Resize listener re-runs placePanel with the stubbed offsetHeight.
  window.dispatchEvent(new Event('resize'))
  await flushPromises()
  return document.querySelector('[data-testid="react-artifact-version-menu"]') as HTMLElement
}

describe('ArtifactVersionSelect', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    vi.restoreAllMocks()
    stubWindowHeight(768)
  })

  it('teleports the menu onto body instead of the trigger wrapper', async () => {
    stubWindowHeight(768)
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
    stubWindowHeight(768)
    const wrapper = mountSelect({ choices: manyChoices(20), currentLabel: 'v20 · 最新', selectedIndex: 20 })
    const menu = await openMenu(wrapper, 320)
    expect(menu.style.maxHeight).toBeTruthy()
    expect(menu.style.overflowY).toBe('auto')
    wrapper.unmount()
  })

  // g2.2: top trigger stays below
  it('opens downward when the trigger is near the top of the viewport', async () => {
    stubWindowHeight(768)
    const wrapper = mountSelect({}, { top: 12, bottom: 32, left: 200, right: 280, width: 80, height: 20 })
    const menu = await openMenu(wrapper, 140)
    expect(menu.getAttribute('data-placement')).toBe('below')
    const top = Number.parseFloat(menu.style.top)
    expect(top).toBeCloseTo(32 + 4, 0)
    wrapper.unmount()
  })

  // g2.1 / screenshot: mid-viewport chip + 5 short versions must hug below, not ≈8px
  it('keeps a short mid-viewport menu below the chip instead of the viewport top', async () => {
    stubWindowHeight(768)
    const triggerTop = 360
    const triggerBottom = 380
    const menuHeight = 150
    const wrapper = mountSelect(
      {
        choices: manyChoices(5),
        currentLabel: 'v5 · 最新',
        selectedIndex: 5,
      },
      { top: triggerTop, bottom: triggerBottom, left: 200, right: 280, width: 80, height: 20 },
    )
    const menu = await openMenu(wrapper, menuHeight)
    expect(menu.getAttribute('data-placement')).toBe('below')
    const top = Number.parseFloat(menu.style.top)
    expect(top).toBeCloseTo(triggerBottom + 4, 0)
    expect(top).toBeGreaterThan(100)
    wrapper.unmount()
  })

  // g2.2: bottom trigger flips above and hugs the chip with actual height
  it('flips above and hugs the chip when there is not enough space below', async () => {
    stubWindowHeight(768)
    const triggerTop = 700
    const triggerBottom = 720
    const menuHeight = 150
    const wrapper = mountSelect(
      {
        choices: manyChoices(5),
        currentLabel: 'v5 · 最新',
        selectedIndex: 5,
      },
      { top: triggerTop, bottom: triggerBottom, left: 200, right: 280, width: 80, height: 20 },
    )
    const menu = await openMenu(wrapper, menuHeight)
    expect(menu.getAttribute('data-placement')).toBe('above')
    const top = Number.parseFloat(menu.style.top)
    // Bottom edge of menu ≈ trigger.top - GAP
    expect(top + menuHeight).toBeCloseTo(triggerTop - 4, 0)
    wrapper.unmount()
  })
})
