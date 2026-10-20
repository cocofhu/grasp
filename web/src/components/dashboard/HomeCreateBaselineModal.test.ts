// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import HomeCreateBaselineModal from './HomeCreateBaselineModal.vue'

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  listProjects: vi.fn(),
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
      listProjects: mocks.listProjects,
      createWorkflowFromBaseline: mocks.createWorkflowFromBaseline,
    },
  }
})

vi.mock('@/lib/composables/useWorkspaceContext', async () => {
  const { ref } = await import('vue')
  return { useWorkspaceContext: () => ({ workspaceId: ref('proj-1'), ensureLoaded: mocks.listProjects }) }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    warn: vi.fn(),
    error: vi.fn(),
    success: mocks.toastSuccess,
  }),
}))


const p1 = { id: 'proj-1', name: 'Grasp', description: '', variables: [] }
const p2 = { id: 'proj-2', name: 'Platform', description: '', variables: [] }
const p3 = { id: 'proj-3', name: 'Demo', description: '', variables: [] }

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
    mocks.listProjects.mockReset()
    mocks.createWorkflowFromBaseline.mockReset()
    mocks.toastSuccess.mockReset()
    mocks.listProjects.mockResolvedValue(p1)
    mocks.createWorkflowFromBaseline.mockResolvedValue({ id: 'wf-new', name: '需求对齐工作流' })
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  // plan g2.2 — exactly one project skips the pick list
  it('skips project list for a single project and focuses the name field', async () => {
    const wrapper = mountModal()
    await flushPromises()
    expect(qExists('home-create-project-list')).toBe(false)
    const name = q('home-create-workflow-name')
    expect(name.element).toBe(document.activeElement)
    await name.setValue('需求对齐工作流')
    expect((name.element as HTMLInputElement).value).toBe('需求对齐工作流')
    expect(document.body.textContent).not.toContain('项目：Grasp')
    expect(document.body.textContent).not.toContain('从零开始')
    wrapper.unmount()
  })

  // plan g2.3 — listProjects failure can retry inside the modal
  it('shows retry when project list fails and does not create', async () => {
    mocks.listProjects.mockRejectedValueOnce(new Error('network down'))
    const wrapper = mountModal()
    await flushPromises()
    expect(q('home-create-load-error').text()).toContain('network down')
    mocks.listProjects.mockResolvedValue(p1)
    await q('home-create-retry').trigger('click')
    await flushPromises()
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
    expect(mocks.createWorkflowFromBaseline).toHaveBeenCalledWith(
      'proj-1',
      '需求对齐工作流',
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
