// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
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

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    warn: vi.fn(),
    error: vi.fn(),
    success: mocks.toastSuccess,
  }),
}))

const dir = dirname(fileURLToPath(import.meta.url))
const modalSrc = readFileSync(join(dir, 'HomeCreateBaselineModal.vue'), 'utf8')
const menuSrc = readFileSync(join(dir, '../workflow/NewWorkflowMenu.vue'), 'utf8')

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
    mocks.listProjects.mockResolvedValue([p1])
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
    expect(document.body.textContent).toContain('项目：Grasp')
    expect(document.body.textContent).not.toContain('从零开始')
    wrapper.unmount()
  })

  // plan g2.1 — two or more projects: pick then form; back returns to pick
  it('uses a two-step wizard when there are multiple projects and can go back', async () => {
    mocks.listProjects.mockResolvedValue([p1, p2, p3])
    const wrapper = mountModal()
    await flushPromises()
    expect(qExists('home-create-project-list')).toBe(true)
    expect(qExists('home-create-workflow-name')).toBe(false)
    expect(q('home-create-pane').classes()).toContain('home-create-step--fwd')
    await q('home-create-project-proj-2').trigger('click')
    await flushPromises()
    expect(qExists('home-create-workflow-name')).toBe(true)
    expect(document.body.textContent).toContain('第 2 / 2 步')
    expect(document.body.textContent).toContain('Platform')
    await q('home-create-workflow-name').setValue('keep-me')
    await q('home-create-back').trigger('click')
    await flushPromises()
    expect(qExists('home-create-project-list')).toBe(true)
    expect(q('home-create-pane').classes()).toContain('home-create-step--back')
    await q('home-create-project-proj-2').trigger('click')
    await flushPromises()
    expect((q('home-create-workflow-name').element as HTMLInputElement).value).toBe('keep-me')
    wrapper.unmount()
  })

  // plan g2.3 — zero projects guides to create a project
  it('guides the user to create a project when none exist', async () => {
    mocks.listProjects.mockResolvedValue([])
    const wrapper = mountModal()
    await flushPromises()
    expect(q('home-create-no-project').text()).toContain('请先创建项目')
    await q('home-create-go-projects').trigger('click')
    expect(mocks.push).toHaveBeenCalledWith('/projects')
    expect(mocks.createWorkflowFromBaseline).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  // plan g2.3 — listProjects failure can retry inside the modal
  it('shows retry when project list fails and does not create', async () => {
    mocks.listProjects.mockRejectedValueOnce(new Error('network down'))
    const wrapper = mountModal()
    await flushPromises()
    expect(q('home-create-load-error').text()).toContain('无法加载项目列表')
    mocks.listProjects.mockResolvedValue([p1, p2])
    await q('home-create-retry').trigger('click')
    await flushPromises()
    expect(qExists('home-create-project-list')).toBe(true)
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

describe('HomeCreateBaselineModal motion + regression (plan g3.4 / g4.2)', () => {
  it('uses transform/opacity step motion and disables translate under reduced motion', () => {
    expect(modalSrc).toMatch(/home-create-step--fwd/)
    expect(modalSrc).toMatch(/home-create-step--back/)
    expect(modalSrc).toMatch(/translateX\(14px\)/)
    expect(modalSrc).toMatch(/translateX\(-14px\)/)
    expect(modalSrc).toMatch(/opacity:\s*0/)
    expect(modalSrc).toMatch(/@media \(prefers-reduced-motion:\s*reduce\)/)
    expect(modalSrc).toMatch(/prefers-reduced-motion[\s\S]*animation:\s*none/)
    expect(modalSrc).not.toMatch(/from-scratch|new-workflow-scratch|从零开始/)
    const appModalSrc = readFileSync(join(dir, '../ui/AppModal.vue'), 'utf8')
    expect(appModalSrc).toMatch(/transition:\s*opacity var\(--dur-overlay\)/)
    expect(appModalSrc).toMatch(/transition:\s*transform var\(--dur-overlay\)/)
    expect(appModalSrc).toMatch(/translateY\(12px\) scale\(0\.98\)/)
  })

  it('keeps project-detail NewWorkflowMenu scratch + baseline options unchanged', () => {
    expect(menuSrc).toMatch(/data-testid="new-workflow-scratch"/)
    expect(menuSrc).toMatch(/data-testid="new-workflow-baseline"/)
    expect(menuSrc).toMatch(/pages\.projectDetail\.newWorkflow\.scratch/)
  })
})
