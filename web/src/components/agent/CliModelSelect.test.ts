// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { CliCatalogModel } from '@/lib/api/apiTypes'
import CliModelSelect from './CliModelSelect.vue'

const catalog: CliCatalogModel[] = [
  { id: 'claude-4.5-sonnet', name: 'Claude Sonnet 4.5', vendor: 'anthropic', tags: ['vision', 'reasoning'], context: 200, recommended: true },
  { id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: ['vision', 'reasoning'], context: 400, recommended: true },
  { id: 'gpt-5-codex', name: 'GPT-5 Codex', vendor: 'openai', tags: ['reasoning'], context: 400 },
]
const mocks = vi.hoisted(() => ({ load: vi.fn(), failed: vi.fn(() => false) }))
vi.mock('@/lib/agent/cliCatalog', () => ({ loadCliModels: mocks.load, cliCatalogFailed: mocks.failed }))

const mounted: { unmount: () => void }[] = []

async function mountSelect(props: { modelValue?: string; backend?: string; variant?: 'full' | 'compact'; emptyMode?: 'auto' | 'required' } = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const w = mount(CliModelSelect, {
    props: { modelValue: '', backend: 'cursor', ...props },
    global: { plugins: [i18n], stubs: { Icon: true } },
    attachTo: document.body,
  })
  mounted.push(w)
  await flushPromises()
  return w
}

const panel = () => document.body.querySelector('[data-testid="cli-model-panel"]') as HTMLElement | null
const q = (sel: string) => document.body.querySelector(sel) as HTMLElement | null

async function openPanel(w: Awaited<ReturnType<typeof mountSelect>>) {
  await w.get('[data-testid="cli-model-trigger"]').trigger('click')
  await flushPromises()
}

async function typeQuery(text: string) {
  const input = q('[data-testid="cli-model-search"]') as HTMLInputElement
  input.value = text
  input.dispatchEvent(new Event('input'))
  await flushPromises()
}

async function key(k: string) {
  q('[data-testid="cli-model-search"]')!.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true }))
  await flushPromises()
}

afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount())
  document.body.innerHTML = ''
  mocks.load.mockReset()
  mocks.failed.mockReset()
  mocks.failed.mockReturnValue(false)
})

describe('CliModelSelect trigger', () => {
  it('shows auto with the bare launch command', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect()
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('自动')
    expect(w.get('[data-testid="cli-model-command"]').text()).toContain('cursor-agent')
    expect(w.find('[data-testid="cli-model-reset"]').exists()).toBe(false)
  })

  it('shows the picked model, its context and the --model flag', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ modelValue: 'gpt-5' })
    const text = w.get('[data-testid="cli-model-trigger"]').text()
    expect(text).toContain('GPT-5')
    expect(text).toContain('400k')
    expect(text).toContain('推荐')
    expect(w.get('[data-testid="cli-model-command"]').text()).toContain('cursor-agent --model gpt-5')
    await w.get('[data-testid="cli-model-reset"]').trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([''])
  })

  it('flags an id outside the shortlist without blocking it', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ modelValue: 'my-private-model' })
    expect(w.get('[data-testid="cli-model-trigger"]').classes()).toContain('border-warn/60')
    expect(w.get('[data-testid="cli-model-unlisted"]').text()).toContain('my-private-model')
  })
})

describe('CliModelSelect panel', () => {
  it('features recommended cards, vendor chips and a detail pane', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ modelValue: 'gpt-5' })
    await openPanel(w)
    expect(panel()).not.toBeNull()
    expect(q('[data-testid="cli-model-card-gpt-5"]')).not.toBeNull()
    expect(q('[data-testid="cli-model-vendor-openai"]')!.textContent).toContain('2')
    expect(q('[data-testid="cli-model-detail"]')!.textContent).toContain('--model gpt-5')
    q('[data-testid="cli-model-card-claude-4.5-sonnet"]')!.click()
    await flushPromises()
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['claude-4.5-sonnet'])
    expect(panel()).toBeNull()
  })

  it('filters by vendor chip and with arrow keys', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect()
    await openPanel(w)
    q('[data-testid="cli-model-vendor-openai"]')!.click()
    await flushPromises()
    expect(q('[data-testid="cli-model-option-claude-4.5-sonnet"]')).toBeNull()
    expect(q('[data-testid="cli-model-option-gpt-5-codex"]')).not.toBeNull()
    await key('ArrowLeft')
    expect(q('[data-testid="cli-model-option-gpt-5-codex"]')).toBeNull()
    expect(q('[data-testid="cli-model-option-claude-4.5-sonnet"]')).not.toBeNull()
    await key('ArrowLeft')
    expect(q('[data-testid="cli-model-vendor-all"]')!.classList.contains('is-on')).toBe(true)
    expect(q('[data-testid="cli-model-option-gpt-5-codex"]')).not.toBeNull()
  })

  it('highlights a query match and picks it with Enter', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect()
    await openPanel(w)
    await typeQuery('codex')
    expect(q('[data-testid="cli-model-card-gpt-5"]')).toBeNull()
    expect(q('[data-testid="cli-model-option-gpt-5-codex"] mark')!.textContent).toBe('Codex')
    await key('Enter')
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['gpt-5-codex'])
  })

  it('offers a typed id below partial matches, and as the empty state otherwise', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect()
    await openPanel(w)
    await typeQuery('gpt')
    expect(q('[data-testid="cli-model-option-typed"]')!.textContent).toContain('gpt')
    await typeQuery('my-model')
    expect(q('[data-testid="cli-model-empty"]')!.textContent).toContain('my-model')
    await key('Enter')
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['my-model'])
  })

  it('walks rows with the arrow keys, starting at the current pick', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ modelValue: 'gpt-5' })
    await openPanel(w)
    await key('ArrowUp')
    await key('Enter')
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['claude-4.5-sonnet'])
  })

  it('picks auto as the empty value', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ modelValue: 'gpt-5' })
    await openPanel(w)
    q('[data-testid="cli-model-option-auto"]')!.click()
    await flushPromises()
    expect(w.emitted('update:modelValue')?.[0]).toEqual([''])
  })

  it('closes on Escape without emitting', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect()
    await openPanel(w)
    await key('Escape')
    expect(panel()).toBeNull()
    expect(w.emitted('update:modelValue')).toBeUndefined()
  })

  it('falls back to typing when the shortlist cannot be read', async () => {
    mocks.load.mockResolvedValue([])
    mocks.failed.mockReturnValue(true)
    const w = await mountSelect()
    await openPanel(w)
    expect(q('[data-testid="cli-model-failed"]')).not.toBeNull()
    expect(q('[data-testid="cli-model-option-auto"]')).not.toBeNull()
  })

  it('hides auto until a concrete model is picked when empty is not a choice', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ emptyMode: 'required' })
    const trigger = w.get('[data-testid="cli-model-trigger"]')
    expect(trigger.text()).toContain('选择一个模型')
    expect(trigger.text()).not.toContain('自动')
    expect(w.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    expect(w.find('[data-testid="cli-model-reset"]').exists()).toBe(false)
    await openPanel(w)
    expect(q('[data-testid="cli-model-option-auto"]')).toBeNull()
    expect(q('[data-testid="cli-model-card-gpt-5"]')).not.toBeNull()
    expect(q('[data-testid="cli-model-vendor-openai"]')).not.toBeNull()
    q('[data-testid="cli-model-vendor-openai"]')!.click()
    await flushPromises()
    expect(q('[data-testid="cli-model-option-claude-4.5-sonnet"]')).toBeNull()
    expect(q('[data-testid="cli-model-option-gpt-5"]')).not.toBeNull()
    await typeQuery('my-private')
    expect(q('[data-testid="cli-model-empty-use"]')).not.toBeNull()
    q('[data-testid="cli-model-empty-use"]')!.click()
    await flushPromises()
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['my-private'])
  })

  it('does not offer reset to auto once a required model is set', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ emptyMode: 'required', modelValue: 'gpt-5' })
    expect(w.find('[data-testid="cli-model-reset"]').exists()).toBe(false)
    expect(w.get('[data-testid="cli-model-command"]').text()).toContain('cursor-agent --model gpt-5')
    await openPanel(w)
    expect(q('[data-testid="cli-model-option-auto"]')).toBeNull()
    expect(q('[data-testid="cli-model-detail"]')!.textContent).not.toContain('自动')
  })

  it('shows details inline in the compact variant', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ variant: 'compact', modelValue: 'gpt-5' })
    await openPanel(w)
    expect(q('[data-testid="cli-model-detail"]')).toBeNull()
    expect(q('[data-testid="cli-model-inline-detail"]')!.textContent).toContain('--model gpt-5')
  })
})
