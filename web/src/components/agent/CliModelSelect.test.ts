// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { CliCatalogModel } from '@/lib/api/apiTypes'
import { theme } from '@/lib/shared/theme'
import claudeLogo from '@/assets/provider-logos/claude.svg'
import geminiLogo from '@/assets/provider-logos/gemini.svg'
import CliModelSelect from './CliModelSelect.vue'

const catalog: CliCatalogModel[] = [
  { id: 'claude-4.5-sonnet', name: 'Claude Sonnet 4.5', vendor: 'anthropic', tags: ['vision', 'reasoning'], context: 200, recommended: true },
  { id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: ['vision', 'reasoning'], context: 400, recommended: true },
  { id: 'gpt-5-codex', name: 'GPT-5 Codex', vendor: 'openai', tags: ['reasoning'], context: 400 },
]
const mocks = vi.hoisted(() => ({ load: vi.fn(), failed: vi.fn(() => false) }))
vi.mock('@/lib/agent/cliCatalog', () => ({ loadCliModels: mocks.load, cliCatalogFailed: mocks.failed }))

const mounted: { unmount: () => void }[] = []

async function mountSelect(
  props: {
    modelValue?: string
    backend?: string
    variant?: 'full' | 'compact'
    hideAutoSub?: boolean
    emptyMode?: 'auto' | 'required'
    hideCommand?: boolean
  } = {},
) {
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
    const trigger = w.get('[data-testid="cli-model-trigger"]').text()
    expect(trigger).toContain('自动')
    expect(trigger).toContain('由 Cursor 决定用哪个模型')
    expect(w.get('[data-testid="cli-model-command"]').text()).toContain('cursor-agent')
    expect(w.get('[data-testid="cli-model-command"]').text()).toContain('不指定 --model，使用 CLI 默认')
    expect(w.find('[data-testid="cli-model-reset"]').exists()).toBe(false)
  })

  it('hides the launch command for meta and keeps 恢复自动', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ hideCommand: true })
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('自动')
    expect(w.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    expect(w.text()).not.toContain('不指定 --model，使用 CLI 默认')
    expect(w.find('[data-testid="cli-model-reset"]').exists()).toBe(false)
    await w.setProps({ modelValue: 'gpt-5' })
    await flushPromises()
    expect(w.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    expect(w.get('[data-testid="cli-model-reset"]').text()).toContain('恢复自动')
    await w.get('[data-testid="cli-model-reset"]').trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([''])
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

  it('hides the auto subtitle when onboarding asks, and still shows a picked model', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ hideAutoSub: true })
    const text = w.get('[data-testid="cli-model-trigger"]').text()
    expect(text).toContain('自动')
    expect(text).not.toContain('由 Cursor 决定用哪个模型')
    expect(w.text()).not.toContain('不确定就先用自动')
    await w.setProps({ modelValue: 'gpt-5' })
    await flushPromises()
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('GPT-5')
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

  it('highlights a compact row without inserting a detail strip', async () => {
    mocks.load.mockResolvedValue(catalog)
    const w = await mountSelect({ variant: 'compact', modelValue: 'gpt-5' })
    await openPanel(w)
    expect(q('[data-testid="cli-model-detail"]')).toBeNull()
    expect(q('[data-testid="cli-model-inline-detail"]')).toBeNull()
    const row = q('[data-testid="cli-model-option-gpt-5"]')!
    expect(row.textContent).toContain('GPT-5')
    expect(row.textContent).toContain('gpt-5')
    expect(row.textContent).not.toContain('日常编码')
    expect(row.textContent).not.toContain('--model')
    row.dispatchEvent(new MouseEvent('mouseenter', { bubbles: true }))
    await flushPromises()
    expect(q('[data-testid="cli-model-inline-detail"]')).toBeNull()
    await key('ArrowDown')
    expect(q('[data-testid="cli-model-inline-detail"]')).toBeNull()
    expect(q('[data-testid="cli-model-option-gpt-5"]')!.textContent).toContain('gpt-5')
  })
})

const cursorTiers: CliCatalogModel[] = [
  { id: 'grok-4.7-xhigh', name: 'Grok 4.7 Extra High', vendor: 'xai' },
  { id: 'grok-4.7-medium', name: 'Grok 4.7 Medium', vendor: 'xai' },
  { id: 'composer-2.5-fast', name: 'Composer 2.5 Fast', vendor: 'cursor', tags: ['fast'] },
  { id: 'claude-opus-5-5-medium', name: 'Claude Opus 5.5 1M', vendor: 'anthropic', context: 1000, recommended: true },
]

describe('CliModelSelect listed Cursor ids', () => {
  it('shows Grok 4.7 Extra High and the cursor-agent flag', async () => {
    mocks.load.mockResolvedValue(cursorTiers)
    const w = await mountSelect({ modelValue: 'grok-4.7-xhigh' })
    const text = w.get('[data-testid="cli-model-trigger"]').text()
    expect(text).toContain('Grok 4.7 Extra High')
    expect(text).toContain('grok-4.7-xhigh')
    expect(text).not.toContain('不在内置清单中')
    expect(w.get('[data-testid="cli-model-trigger"]').classes()).not.toContain('border-warn/60')
    expect(w.get('[data-testid="cli-model-command"]').text()).toContain('cursor-agent --model grok-4.7-xhigh')
  })

  it('finds the tier by id or name, and hides the custom row on an exact id', async () => {
    mocks.load.mockResolvedValue(cursorTiers)
    const w = await mountSelect()
    await openPanel(w)
    await typeQuery('grok-4.7-xhigh')
    expect(q('[data-testid="cli-model-option-grok-4.7-xhigh"]')).not.toBeNull()
    expect(q('[data-testid="cli-model-option-typed"]')).toBeNull()
    await typeQuery('Grok 4.7 Extra High')
    expect(q('[data-testid="cli-model-option-grok-4.7-xhigh"]')).not.toBeNull()
    await typeQuery('')
    q('[data-testid="cli-model-vendor-xai"]')!.click()
    await flushPromises()
    expect(q('[data-testid="cli-model-option-grok-4.7-xhigh"]')).not.toBeNull()
    expect(q('[data-testid="cli-model-option-composer-2.5-fast"]')).toBeNull()
    q('[data-testid="cli-model-option-grok-4.7-xhigh"]')!.click()
    await flushPromises()
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['grok-4.7-xhigh'])
  })
})

const opencodeCatalog: CliCatalogModel[] = [
  { id: 'opencode/gemini-3.5-flash', name: 'Gemini 3.5 Flash', vendor: 'opencode' },
  { id: 'opencode/claude-sonnet-5', name: 'Claude Sonnet 5', vendor: 'opencode' },
  { id: 'opencode/custom-private', name: 'Private model', vendor: 'opencode' },
]

describe('CliModelSelect OpenCode family marks', () => {
  it('uses model family marks and a letter for an unfamiliar OpenCode id', async () => {
    mocks.load.mockResolvedValue(opencodeCatalog)
    const previous = theme.value
    const w = await mountSelect({ variant: 'compact', modelValue: 'opencode/gemini-3.5-flash', backend: 'opencode' })
    try {
      const triggerMark = w.get('[data-testid="cli-model-trigger"] [data-vendor-mark="google"]')
      expect(triggerMark.get('img').attributes('src')).toBe(geminiLogo)
      expect(triggerMark.classes()).toContain('vendor-mark--color')
      for (const mode of ['light', 'dark'] as const) {
        theme.value = mode
        await flushPromises()
        expect(triggerMark.get('img').classes()).not.toContain('invert')
      }
      await openPanel(w)
      const gemini = q('[data-testid="cli-model-option-opencode/gemini-3.5-flash"]')!
      const claude = q('[data-testid="cli-model-option-opencode/claude-sonnet-5"]')!
      const providerFallback = q('[data-testid="cli-model-option-opencode/custom-private"]')!
      expect(gemini.querySelector('[data-vendor-mark="google"] img')!.getAttribute('src')).toBe(geminiLogo)
      expect(gemini.querySelector('[data-vendor-mark="google"]')!.classList.contains('vendor-mark--color')).toBe(true)
      expect(gemini.querySelector('[data-vendor-mark="google"] img')!.classList.contains('invert')).toBe(false)
      expect(claude.querySelector('[data-vendor-mark="anthropic"] img')!.getAttribute('src')).toBe(claudeLogo)
      expect(claude.querySelector('[data-vendor-mark="anthropic"] img')!.getAttribute('src')).not.toBe(geminiLogo)
      const monogram = providerFallback.querySelector('[data-vendor-mark="monogram"]')!
      expect(monogram.querySelector('img')).toBeNull()
      expect(monogram.textContent?.trim()).toBe('O')
      expect(providerFallback.querySelector('[data-vendor-mark="google"]')).toBeNull()
      expect(providerFallback.querySelector('[data-vendor-mark="anthropic"]')).toBeNull()
      expect(q('[data-testid="cli-model-inline-detail"]')).toBeNull()
      expect(gemini.textContent).toContain('opencode/gemini-3.5-flash')
    } finally {
      theme.value = previous
    }
  })

  it('writes the original id, matches search, and keeps the selected check', async () => {
    mocks.load.mockResolvedValue(opencodeCatalog)
    const w = await mountSelect({ variant: 'compact', modelValue: 'opencode/gemini-3.5-flash', backend: 'opencode', hideCommand: true })
    await openPanel(w)
    expect(q('[data-testid="cli-model-option-opencode/gemini-3.5-flash"] icon-stub[name="check"]')).not.toBeNull()
    expect(q('[data-testid="cli-model-option-opencode/claude-sonnet-5"] icon-stub[name="check"]')).toBeNull()
    await typeQuery('Claude Sonnet')
    expect(q('[data-testid="cli-model-option-opencode/claude-sonnet-5"]')).not.toBeNull()
    expect(q('[data-testid="cli-model-option-opencode/gemini-3.5-flash"]')).toBeNull()
    await typeQuery('opencode/gemini-3.5-flash')
    expect(q('[data-testid="cli-model-option-opencode/gemini-3.5-flash"]')).not.toBeNull()
    expect(q('[data-testid="cli-model-option-typed"]')).toBeNull()
    await typeQuery('claude-sonnet-5')
    q('[data-testid="cli-model-option-opencode/claude-sonnet-5"]')!.click()
    await flushPromises()
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['opencode/claude-sonnet-5'])
    expect(panel()).toBeNull()
  })
})
