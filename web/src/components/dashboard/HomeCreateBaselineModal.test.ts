// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import HomeCreateBaselineModal from './HomeCreateBaselineModal.vue'

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  ensureLoaded: vi.fn(),
  createWorkflowFromBaseline: vi.fn(),
  toastSuccess: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mocks.push }),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      createWorkflowFromBaseline: mocks.createWorkflowFromBaseline,
    },
  }
})

vi.mock('@/lib/composables/useWorkspaceContext', async () => {
  const { ref } = await import('vue')
  return { useWorkspaceContext: () => ({ workspace: ref(null), ensureLoaded: mocks.ensureLoaded }) }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    warn: vi.fn(),
    error: vi.fn(),
    success: mocks.toastSuccess,
  }),
}))

const workspace = { name: 'Grasp', description: '', variables: [] }

function q(testid: string) {
  const el = document.querySelector(`[data-testid="${testid}"]`)
  if (!el) throw new Error(`missing [data-testid="${testid}"]`)
  return new DOMWrapper(el)
}

function qExists(testid: string) {
  return document.querySelector(`[data-testid="${testid}"]`) != null
}

function mountModal(open = true) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(HomeCreateBaselineModal, {
    props: { open },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: { Teleport: false, Transition: false },
    },
  })
}

describe('HomeCreateBaselineModal (plan g2 / g3 / g4)', () => {
  beforeEach(() => {
    mocks.push.mockReset()
    mocks.ensureLoaded.mockReset()
    mocks.createWorkflowFromBaseline.mockReset()
    mocks.toastSuccess.mockReset()
    mocks.ensureLoaded.mockResolvedValue(workspace)
    mocks.createWorkflowFromBaseline.mockResolvedValue({ id: 'wf-new', name: '需求对齐工作流' })
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  // plan g2.2 — the form opens directly once the workspace is loaded
  it('loads the workspace and focuses the name field', async () => {
    const wrapper = mountModal()
    await flushPromises()
    expect(mocks.ensureLoaded).toHaveBeenCalledTimes(1)
    const name = q('home-create-workflow-name')
    expect(name.element).toBe(document.activeElement)
    await name.setValue('需求对齐工作流')
    expect((name.element as HTMLInputElement).value).toBe('需求对齐工作流')
    expect(document.body.textContent).not.toContain('从零开始')
    wrapper.unmount()
  })

  // plan g2.3 — workspace load failure can retry inside the modal
  it('shows retry when the workspace fails to load and does not create', async () => {
    mocks.ensureLoaded.mockResolvedValueOnce(null)
    const wrapper = mountModal()
    await flushPromises()
    expect(q('home-create-load-error').text()).toContain('无法加载工作区，请重试')
    expect(qExists('home-create-form')).toBe(false)
    await q('home-create-retry').trigger('click')
    await flushPromises()
    expect(mocks.ensureLoaded).toHaveBeenCalledTimes(2)
    expect(qExists('home-create-form')).toBe(true)
    expect(mocks.createWorkflowFromBaseline).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  // plan g2.4 — name required, at least one repo URL, no scratch path
  it('disables submit until name and a repo URL are filled, then creates via baseline API', async () => {
    const wrapper = mountModal()
    await flushPromises()
    const submit = q('home-create-submit')
    expect((submit.element as HTMLButtonElement).disabled).toBe(true)
    await q('home-create-workflow-name').setValue('需求对齐工作流')
    await flushPromises()
    expect((q('home-create-submit').element as HTMLButtonElement).disabled).toBe(true)
    const url = document.querySelector('input[placeholder*="https"]') as HTMLInputElement | null
    expect(url).toBeTruthy()
    await new DOMWrapper(url!).setValue('https://github.com/org/repo')
    await flushPromises()
    expect((q('home-create-submit').element as HTMLButtonElement).disabled).toBe(false)
    await q('home-create-submit').trigger('click')
    await flushPromises()
    expect(mocks.createWorkflowFromBaseline).toHaveBeenCalledWith('需求对齐工作流',
      expect.arrayContaining([expect.objectContaining({ url: 'https://github.com/org/repo' })]),
    )
    expect(mocks.toastSuccess).toHaveBeenCalled()
    expect(wrapper.emitted('created')).toEqual([[{ id: 'wf-new', name: '需求对齐工作流' }]])
    expect(wrapper.emitted('close')).toBeTruthy()
    wrapper.unmount()
  })

  // plan g2.2 — create failure restores a clickable submit and does not emit created
  it('keeps the form open and restores submit after a create failure', async () => {
    mocks.createWorkflowFromBaseline.mockRejectedValue(new Error('create failed'))
    const wrapper = mountModal()
    await flushPromises()
    await q('home-create-workflow-name').setValue('X')
    const url = document.querySelector('input[placeholder*="https"]') as HTMLInputElement | null
    await new DOMWrapper(url!).setValue('https://example.com/r.git')
    await flushPromises()
    await q('home-create-submit').trigger('click')
    await flushPromises()
    expect(q('home-create-error').text()).toContain('create failed')
    expect((q('home-create-submit').element as HTMLButtonElement).disabled).toBe(false)
    expect(wrapper.emitted('created')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeFalsy()
    wrapper.unmount()
  })
})
