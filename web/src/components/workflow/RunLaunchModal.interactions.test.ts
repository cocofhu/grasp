// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({ startRun: vi.fn(), listRunTags: vi.fn() }))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, startRun: mocks.startRun, listRunTags: mocks.listRunTags } }
})
import RunLaunchModal from './RunLaunchModal.vue'

const fields = [
  { key: 'sel', type: 'select', options: ' a,，b ', required: true },
  { key: 'bool', type: 'bool' },
  { key: 'num', type: 'number' },
  { key: 'text', type: 'text' },
  { key: 'para', type: 'paragraph', required: true },
  { key: 'repos', type: 'repos' },
  { key: 'other' },
]
function mountModal(extra: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(RunLaunchModal, {
    props: {
      open: true, workflowId: 'wf', workflowName: 'Flow', fields,
      runInputs: { sel: 'a', bool: 'false', num: '2', text: 'x', para: 'body', repos: '[{"name":"api","url":"u"}]', other: 'y' },
      runImages: { para: [] }, firstMessage: null, ...extra,
    },
    attachTo: document.body,
    global: { plugins: [i18n], stubs: {
      Icon: true,
      AppButton: { template: '<button v-bind="$attrs"><slot /></button>' },
      AppSwitch: { props: ['modelValue', 'disabled'], emits: ['update:modelValue'], template: '<button role="switch" @click="$emit(\'update:modelValue\', !modelValue)" />' },
      AppModal: { props: ['open'], emits: ['close'], setup(_: any, { slots, expose }: any) { const scrollAreaEl = document.createElement('div'); Object.defineProperty(scrollAreaEl, 'clientHeight', { value: 240 }); expose({ scrollAreaEl }); return () => _.open ? slots.default?.().concat(slots.footer?.() || []) : null } },
      ParagraphInput: { props: ['text'], emits: ['update:text'], template: '<textarea :value="text" />' },
      ReposEditor: { name: 'ReposEditor', props: ['repos'], emits: ['update:repos'], template: '<div data-testid="repos-stub"/>' },
      PrioritySegmented: { props: ['modelValue'], emits: ['update:modelValue'], template: '<button data-testid="priority" @click="$emit(\'update:modelValue\', \'high\')" />' },
      HardLoadLayer: { emits: ['retry'], template: '<button data-testid="retry" @click="$emit(\'retry\')" />' },
    } },
  })
}

describe('RunLaunchModal interactions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.listRunTags.mockResolvedValue({ tags: ['bug', 'feature'] })
    mocks.startRun.mockResolvedValue({ id: 'run-1' })
  })

  it('drives field helpers, repositories, booleans, tags and env rows', async () => {
    const w = mountModal()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.fieldOptions(fields[0])).toEqual(['a', 'b'])
    expect(vm.reposFor('repos')[0].name).toBe('api')
    vm.onReposUpdate('repos', [{ name: 'web', url: 'v', branch: 'main' }])
    expect(JSON.parse(w.props('runInputs').repos)[0].name).toBe('web')
    vm.setBoolField(fields[1], true)
    expect(vm.boolDisplayValue('bool')).toBe('true')
    vm.setBoolField({ key: 'bool', editable: false }, false)
    expect(w.props('runInputs').bool).toBe('true')
    vm.addTag(' bug ')
    vm.addTag('bug')
    vm.tagInput = 'feature'
    vm.onTagKeydown(new KeyboardEvent('keydown', { key: 'Enter' }))
    vm.onTagKeydown(new KeyboardEvent('keydown', { key: 'Backspace' }))
    vm.removeTag('bug')
    vm.addEnvRow()
    vm.setEnvSecret(0, true)
    expect(vm.envRows[0].secret).toBe(true)
    vm.setEnvSecret(9, true)
    vm.removeEnvRow(0)
    expect(vm.envRows).toEqual([])
    w.unmount()
  })

  it('submits composite inputs, metadata and handles success keyboard/navigation', async () => {
    const beforeStart = vi.fn().mockResolvedValue(undefined)
    const w = mountModal({ beforeStart, runTitle: ' title ', firstMessage: { text: 'hello' } })
    await flushPromises()
    const vm = w.vm as any
    vm.priority = 'high'
    vm.tags = ['bug']
    vm.envRows = [{ key: 'LOG_LEVEL', value: 'debug', secret: false }]
    await vm.startRun()
    await flushPromises()
    expect(beforeStart).toHaveBeenCalled()
    expect(mocks.startRun).toHaveBeenCalledWith('wf', expect.objectContaining({ para: expect.anything() }), 'manual', 'high', ['bug'], expect.objectContaining({
      env: [{ key: 'LOG_LEVEL', value: 'debug', secret: false }], title: 'title', firstMessage: { text: 'hello' },
    }))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    expect(w.emitted('view-run')?.[0]).toEqual(['run-1'])
    expect(w.emitted('close')).toBeTruthy()
    w.unmount()
  })

  it('covers malformed repos, validation, close phases and retry errors', async () => {
    const w = mountModal({ runInputs: { sel: '', para: '', repos: '{bad' }, runImages: { para: [] } })
    await flushPromises()
    const vm = w.vm as any
    expect(vm.reposFor('repos')).toEqual([])
    await vm.startRun()
    expect(vm.phase).toBe('error')
    vm.envRows = [{ key: 'CURSOR_API_KEY', value: 'x', secret: true }]
    w.props('runInputs').sel = 'a'
    w.props('runInputs').para = 'ok'
    await vm.startRun()
    expect(vm.phase).toBe('error')
    mocks.startRun.mockRejectedValueOnce(new Error('boom'))
    vm.envRows = []
    await vm.startRun()
    expect(vm.startError).toBe('boom')
    vm.phase = 'form'
    vm.onModalClose()
    vm.phase = 'success'; vm.successRunId = 'r2'; vm.onModalClose()
    expect(w.emitted('stayed')).toBeTruthy()
    vm.phase = 'loading'; vm.onModalClose()
    expect(vm.bodyLayerClass(false)).toContain('hidden')
    w.unmount()
  })

  it('resets all transient state when reopened and handles tag limits', async () => {
    const w = mountModal()
    await flushPromises()
    const vm = w.vm as any
    vm.tags = Array.from({ length: vm.maxTags }, (_, i) => `t${i}`)
    vm.addTag('overflow')
    expect(vm.tags).toHaveLength(vm.maxTags)
    vm.tagInput = 'bad tag!'
    expect(vm.tagError).toBeTruthy()
    await w.setProps({ open: false })
    await w.setProps({ open: true })
    await flushPromises()
    expect(vm.tags).toEqual([])
    expect(vm.loadingLayerMinHeight).toBeUndefined()
    w.unmount()
  })
})
