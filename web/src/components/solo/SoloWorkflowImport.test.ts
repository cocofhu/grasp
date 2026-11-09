// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import SoloWorkflowImport, { type WorkspaceImportDraft } from './SoloWorkflowImport.vue'

const mocks = vi.hoisted(() => ({ req: vi.fn() }))
vi.mock('@/lib/api/httpCore', () => ({ req: mocks.req }))
const source = { nodeId: 'implement', iteration: 2, attempt: 1, shared: false, available: true, state: 'running', createdAt: '2026-11-09T09:00:00Z' }
const repo = { path: 'app', commit: 'a'.repeat(40), head: 'a'.repeat(40), supported: true, dirty: true, untracked: true, files: 3, bytes: 1024 }
const receipt = (state = 'prepared') => ({ id: 'opaque-preview-receipt', source: { runId: 'run-1', nodeId: source.nodeId, iteration: 2, attempt: 1 }, state, repositories: [repo], directory: state === 'applied' ? '.grasp-code-imports/folder' : undefined, paths: state === 'applied' ? ['.grasp-code-imports/folder/app'] : [], createdAt: '2026-11-09T09:00:00Z' })
const base = '/solo/sessions/solo-1/workspace-imports'
const wrappers: ReturnType<typeof mount>[] = []
let current = receipt()
let saved: ReturnType<typeof receipt>[] = []
function open(draft: WorkspaceImportDraft | null = null) {
  const wrapper = mount(SoloWorkflowImport, {
    props: { sessionId: 'solo-1', runId: 'run-1', nodes: [{ id: 'implement', label: '实现' }], draft },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })], directives: { 'hover-ink': {} } },
  })
  wrappers.push(wrapper)
  return wrapper
}
function held(wrapper: ReturnType<typeof open>) { return wrapper.emitted('update:draft')!.at(-1)![0] as WorkspaceImportDraft }
function writes() { return mocks.req.mock.calls.filter(([, init]) => init?.method === 'POST') }
async function prepare(wrapper: ReturnType<typeof open>) {
  await flushPromises()
  await wrapper.get('[data-testid="code-import-source"]').setValue(JSON.stringify([source.nodeId, 2, 1]))
  await wrapper.get('[data-testid="code-import-inventory"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="code-import-preview"]').trigger('click')
  await flushPromises()
}
beforeEach(() => {
  current = receipt()
  saved = []
  mocks.req.mockReset()
  mocks.req.mockImplementation(async (path: string, init?: RequestInit) => {
    if (path === `${base}/sources?runId=run-1`) return [source]
    if (path === `${base}?runId=run-1`) return saved
    if (path === `${base}/inventory`) return { version: 1, repositories: [repo], workingChanges: false, nonGit: false, ignoredFiles: false }
    if (path === base && init?.method === 'POST') { saved = [current]; return structuredClone(current) }
    if (path.endsWith('/confirm')) { current = receipt('applied'); return structuredClone(current) }
    if (path.endsWith('/reconcile')) { current = receipt('applied'); return structuredClone(current) }
    if (path === `${base}/${current.id}`) return structuredClone(current)
    throw new Error(`unexpected ${path}`)
  })
})
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount() })

describe('workflow code import', () => {
  it('initializes an initially empty bound drawer model before its first render', async () => {
    const retained = ref<WorkspaceImportDraft | null>(null)
    const host = mount(defineComponent({ setup: () => () => h(SoloWorkflowImport, { sessionId: 'solo-1', runId: 'run-1', draft: retained.value, 'onUpdate:draft': value => { retained.value = value } }) }), { global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })], directives: { 'hover-ink': {} } } })
    wrappers.push(host)
    await flushPromises()
    expect(host.get('[data-testid="code-import-source"]').exists()).toBe(true)
    expect(retained.value?.preview).toBeNull()
    await host.get('[data-testid="code-import-source"]').setValue(JSON.stringify([source.nodeId, 2, 1]))
    expect(retained.value?.sourceKey).toBe(JSON.stringify([source.nodeId, 2, 1]))
  })

  it('requires selecting an execution, previewing and a separate explicit confirmation', async () => {
    const wrapper = open()
    await flushPromises()
    expect(writes()).toHaveLength(0)
    expect(wrapper.text()).toContain('未提交改动')
    expect(wrapper.text()).toContain('非 Git')
    await prepare(wrapper)
    expect(writes().map(([path]) => path)).toEqual([`${base}/inventory`, base])
    const previewBody = JSON.parse(writes()[1]![1]!.body as string)
    expect(previewBody).toMatchObject({ runId: 'run-1', nodeId: 'implement', iteration: 2, attempt: 1, repositories: [{ path: 'app', commit: repo.commit }] })
    expect(previewBody.clientRequestId).toBeTruthy()
    expect(wrapper.text()).not.toContain('opaque-preview-receipt')
    expect(wrapper.text()).toContain('这些文件保留在来源')
    await wrapper.get('[data-testid="code-import-confirm"]').trigger('click')
    await flushPromises()
    const [path, init] = writes().at(-1)!
    expect(path).toBe(`${base}/opaque-preview-receipt/confirm`)
    expect(Object.keys(JSON.parse(init!.body as string)).sort()).toEqual(['clientRequestId', 'confirmed'])
    expect(JSON.parse(init!.body as string).confirmed).toBe(true)
    expect(wrapper.get('[data-testid="code-import-result-path"]').text()).toBe('.grasp-code-imports/folder/app')
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.get('[data-testid="code-import-previous"]').text()).toContain('已导入独立目录')
  })

  it('retains the same frozen preview request on a failed response', async () => {
    const standard = mocks.req.getMockImplementation()!
    const bodies: unknown[] = []
    mocks.req.mockImplementation(async (path: string, init?: RequestInit) => {
      if (path === base && init?.method === 'POST') { bodies.push(JSON.parse(init.body as string)); if (bodies.length === 1) throw new Error('预览响应中断') }
      return standard(path, init)
    })
    const wrapper = open()
    await prepare(wrapper)
    expect(wrapper.text()).toContain('预览响应中断')
    await wrapper.get('[data-testid="code-import-preview"]').trigger('click')
    await flushPromises()
    expect(bodies).toHaveLength(2)
    expect(bodies[1]).toEqual(bodies[0])
    expect(writes().filter(([path]) => path.endsWith('/confirm'))).toHaveLength(0)
  })

  it('preserves repository and commit drafts when the drawer is closed before preview', async () => {
    const first = open()
    await flushPromises()
    await first.get('[data-testid="code-import-source"]').setValue(JSON.stringify([source.nodeId, 2, 1]))
    await first.get('[data-testid="code-import-inventory"]').trigger('click')
    await flushPromises()
    await first.get('input[aria-label="app 完整 Git 提交 SHA"]').setValue('b'.repeat(40))
    const retained = held(first)
    first.unmount()
    const second = open(retained)
    await flushPromises()
    expect((second.get('input[aria-label="app 完整 Git 提交 SHA"]').element as HTMLInputElement).value).toBe('b'.repeat(40))
    expect(writes().map(([path]) => path)).toEqual([`${base}/inventory`])
    expect(second.find('[data-testid="code-import-confirm"]').exists()).toBe(false)
  })

  it('keeps an uncertain confirmation through drawer reopen and checks without replaying import', async () => {
    const standard = mocks.req.getMockImplementation()!
    mocks.req.mockImplementation(async (path: string, init?: RequestInit) => {
      if (path.endsWith('/confirm')) { current = receipt('unknown'); throw new Error('连接中断') }
      return standard(path, init)
    })
    const first = open()
    await prepare(first)
    await first.get('[data-testid="code-import-confirm"]').trigger('click')
    await flushPromises()
    const draft = held(first)
    expect(draft.uncertain).toBe(true)
    const confirmation = writes().filter(([path]) => path.endsWith('/confirm'))[0]
    first.unmount()
    const second = open(draft)
    await flushPromises()
    expect(writes().filter(([path]) => path.endsWith('/confirm'))).toEqual([confirmation])
    expect(second.find('[data-testid="code-import-confirm"]').exists()).toBe(false)
    expect(second.get('[data-testid="code-import-reconcile"]').exists()).toBe(true)
    expect(writes().filter(([path]) => path.endsWith('/reconcile'))).toHaveLength(0)
    await second.get('[data-testid="code-import-reconcile"]').trigger('click')
    await flushPromises()
    expect(writes().filter(([path]) => path.endsWith('/confirm'))).toHaveLength(1)
    expect(writes().filter(([path]) => path.endsWith('/reconcile'))).toHaveLength(1)
    expect(second.text()).toContain('.grasp-code-imports/folder/app')
  })

  it('recovers saved previews after page reload using read APIs only', async () => {
    current = receipt('unknown')
    saved = [current]
    const wrapper = open()
    await flushPromises()
    expect(writes()).toHaveLength(0)
    await wrapper.get('[data-testid="code-import-previous"]').trigger('click')
    await flushPromises()
    expect(writes()).toHaveLength(0)
    expect(wrapper.find('[data-testid="code-import-confirm"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="code-import-reconcile"]').exists()).toBe(true)
  })

  it('does not silently enable unsupported repositories or accept a branch as a frozen commit', async () => {
    const standard = mocks.req.getMockImplementation()!
    mocks.req.mockImplementation(async (path: string, init?: RequestInit) => path.endsWith('/inventory') ? { version: 1, repositories: [repo, { ...repo, path: 'non-git', supported: false }], workingChanges: false } : standard(path, init))
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="code-import-source"]').setValue(JSON.stringify([source.nodeId, 2, 1]))
    await wrapper.get('[data-testid="code-import-inventory"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('input[aria-label="non-git"]').attributes('disabled')).toBeDefined()
    await wrapper.get('input[aria-label="app 完整 Git 提交 SHA"]').setValue('main')
    expect(wrapper.get('[data-testid="code-import-preview"]').attributes('disabled')).toBeDefined()
    expect(writes().filter(([path]) => path === base)).toHaveLength(0)
  })

  it.each([
    ['repository_lfs', '通过 Git LFS'],
    ['repository_submodules', '包含子模块'],
    ['repository_shallow', '只保留了部分 Git 历史'],
    ['repository_custom_configuration', '自定义文件转换或外部 Git 配置'],
    ['repository_limit', '超过导入限额'],
    ['private runtime path /internal/secret', '当前仓库不支持通过 Git 提交方式导入'],
  ])('explains unavailable repositories using safe localized reasons: %s', async (reason, message) => {
    const standard = mocks.req.getMockImplementation()!
    mocks.req.mockImplementation(async (path: string, init?: RequestInit) => path.endsWith('/inventory')
      ? { version: 1, repositories: [{ ...repo, supported: false, reason }], workingChanges: false }
      : standard(path, init))
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="code-import-source"]').setValue(JSON.stringify([source.nodeId, 2, 1]))
    await wrapper.get('[data-testid="code-import-inventory"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain(message)
    expect(wrapper.text()).not.toContain('/internal/secret')
    expect(wrapper.get('input[aria-label="app"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="code-import-preview"]').attributes('disabled')).toBeDefined()
  })
})
