// @vitest-environment happy-dom
import { computed, defineComponent, h, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { ACP_BACKENDS, getRegionPolicy, type BackendId } from '@/lib/shared/regionPolicy'
import CodingToolPicker from './CodingToolPicker.vue'

const mounted: { unmount: () => void }[] = []
function setup(options: { backend?: BackendId; currentRegion?: string; specialRegion?: string; disabled?: boolean; testIdPrefix?: string } = {}) {
  const backend = ref<BackendId>(options.backend || 'cursor')
  const currentRegion = ref(options.currentRegion || '')
  const policy = computed(() => getRegionPolicy(backend.value))
  const Host = defineComponent({
    setup: () => () => h(CodingToolPicker, {
      modelValue: backend.value,
      regionPolicy: policy.value,
      currentRegion: currentRegion.value,
      specialRegion: options.specialRegion || '',
      disabled: options.disabled,
      testIdPrefix: options.testIdPrefix || 'acp',
      'onUpdate:modelValue': (value: BackendId) => { backend.value = value },
      onSelectRegion: (value: string) => { currentRegion.value = value },
    }),
  })
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const wrapper = mount(Host, { attachTo: document.body, global: { plugins: [i18n] } })
  mounted.push(wrapper)
  return { wrapper, backend, currentRegion, picker: wrapper.getComponent(CodingToolPicker) }
}

afterEach(() => { mounted.splice(0).forEach((wrapper) => wrapper.unmount()); document.body.innerHTML = '' })

describe('CodingToolPicker', () => {
  it('offers all seven tools with one selected radio and lets users choose OpenCode', async () => {
    const { wrapper, picker, backend } = setup({ testIdPrefix: 'agent-wizard' })
    const tools = wrapper.get('[role="radiogroup"]').findAll('[role="radio"]')
    expect(tools).toHaveLength(7)
    expect(tools.filter((tool) => tool.attributes('aria-checked') === 'true')).toHaveLength(1)
    expect(tools.filter((tool) => tool.attributes('tabindex') === '0')).toHaveLength(1)
    expect(tools.every((tool) => tool.attributes('aria-pressed') === undefined)).toBe(true)
    for (const tool of ACP_BACKENDS) {
      const row = wrapper.get(`[data-testid="agent-wizard-backend-${tool.id}"]`)
      expect(row.text()).toBe(tool.label)
      expect(row.find('[data-provider-logo][aria-hidden="true"]').exists()).toBe(true)
    }
    expect(wrapper.find('[data-testid="coding-tool-site"]').exists()).toBe(false)
    await wrapper.get('[data-testid="agent-wizard-backend-opencode"]').trigger('click')
    expect(backend.value).toBe('opencode')
    expect(picker.emitted('update:modelValue')).toEqual([['opencode']])
    expect(wrapper.get('[data-testid="agent-wizard-backend-opencode"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="agent-wizard-backend-cursor"]').attributes('aria-checked')).toBe('false')
  })

  it('moves tool selection and focus with wrapping arrows, Home and End', async () => {
    const { wrapper, backend } = setup()
    const cursor = wrapper.get('[data-testid="acp-backend-cursor"]')
    ;(cursor.element as HTMLButtonElement).focus()
    await cursor.trigger('keydown', { key: 'ArrowUp' })
    await flushPromises()
    expect(backend.value).toBe('deepseek_harness')
    expect(document.activeElement).toBe(wrapper.get('[data-testid="acp-backend-deepseek_harness"]').element)
    await wrapper.get('[data-testid="acp-backend-deepseek_harness"]').trigger('keydown', { key: 'Home' })
    await flushPromises()
    expect(backend.value).toBe('cursor')
    expect(document.activeElement).toBe(cursor.element)
    await cursor.trigger('keydown', { key: 'End' })
    await flushPromises()
    expect(backend.value).toBe('deepseek_harness')
    await wrapper.get('[data-testid="acp-backend-deepseek_harness"]').trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    expect(backend.value).toBe('cursor')
    expect(document.activeElement).toBe(cursor.element)
  })

  it('selects a site by pointer and keyboard without changing the coding tool', async () => {
    const { wrapper, picker, backend, currentRegion } = setup({ backend: 'codebuddy', currentRegion: 'public' })
    const domestic = wrapper.get('[data-testid="acp-region-internal"]')
    const international = wrapper.get('[data-testid="acp-region-public"]')
    expect(domestic.attributes('role')).toBe('radio')
    expect(domestic.attributes('aria-label')).toBe('国内站 (internal)')
    expect(international.attributes('aria-label')).toBe('国际站 (public)')
    expect(international.attributes('aria-checked')).toBe('true')
    await domestic.trigger('click')
    expect(currentRegion.value).toBe('internal')
    expect(domestic.attributes('aria-checked')).toBe('true')
    expect(international.attributes('aria-checked')).toBe('false')
    await domestic.trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(currentRegion.value).toBe('public')
    expect(document.activeElement).toBe(international.element)
    expect(picker.emitted('selectRegion')).toEqual([['internal'], ['public']])
    expect(backend.value).toBe('codebuddy')
  })

  it('shows an existing special site without silently selecting a normal site', async () => {
    const { wrapper, picker, currentRegion } = setup({ backend: 'codebuddy', specialRegion: 'legacy-zone' })
    expect(wrapper.get('[data-testid="coding-tool-site"] [role="status"]').text()).toContain('legacy-zone')
    const sites = wrapper.get('[data-testid="coding-tool-site"]').findAll('[role="radio"]')
    expect(sites.every((site) => site.attributes('aria-checked') === 'false')).toBe(true)
    expect(currentRegion.value).toBe('')
    expect(picker.emitted('selectRegion')).toBeUndefined()
    await wrapper.get('[data-testid="acp-region-internal"]').trigger('click')
    expect(currentRegion.value).toBe('internal')
  })

  it('blocks tool and site changes while disabled', async () => {
    const { wrapper, picker, backend, currentRegion } = setup({ backend: 'codebuddy', currentRegion: 'public', disabled: true })
    expect(wrapper.findAll('[role="radio"]').every((radio) => radio.attributes('disabled') !== undefined)).toBe(true)
    await wrapper.get('[data-testid="acp-backend-opencode"]').trigger('click')
    await wrapper.get('[data-testid="acp-backend-codebuddy"]').trigger('keydown', { key: 'ArrowDown' })
    await wrapper.get('[data-testid="acp-region-internal"]').trigger('click')
    await wrapper.get('[data-testid="acp-region-public"]').trigger('keydown', { key: 'ArrowLeft' })
    expect(backend.value).toBe('codebuddy')
    expect(currentRegion.value).toBe('public')
    expect(picker.emitted('update:modelValue')).toBeUndefined()
    expect(picker.emitted('selectRegion')).toBeUndefined()
  })
})
