// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import messages from '@/locales/zh-CN/workspaceRelease.json'
import SoloEnvironmentRelease, { type EnvironmentReleaseDraft } from './SoloEnvironmentRelease.vue'

const mocks = vi.hoisted(() => ({ req: vi.fn() }))
vi.mock('@/lib/api/httpCore', () => ({ req: mocks.req }))
const base = '/solo/sessions/solo-1/environment-releases'
const receipt = (state = 'prepared') => ({ id: 'private-original-receipt', state, wholeEnvironment: true, chatsRetained: true, snapshotsRetained: 2, returnPointsRetained: 1, workspacesAffected: 3, lossConfirmationRequired: true, requiresReconciliation: ['dispatching', 'unknown'].includes(state), createdAt: '2026-11-09T12:00:00Z' })
let current = receipt()
let saved: ReturnType<typeof receipt>[] = []
const wrappers: ReturnType<typeof mount>[] = []
function open(draft: EnvironmentReleaseDraft | null = null) {
  const wrapper = mount(SoloEnvironmentRelease, { props: { sessionId: 'solo-1', draft }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': messages } })], directives: { 'hover-ink': {} } } })
  wrappers.push(wrapper)
  return wrapper
}
function held(wrapper: ReturnType<typeof open>) { return wrapper.emitted('update:draft')!.at(-1)![0] as EnvironmentReleaseDraft }
function writes() { return mocks.req.mock.calls.filter(([, init]) => init?.method === 'POST') }
async function prepare(wrapper: ReturnType<typeof open>) {
  await flushPromises()
  await wrapper.get('[data-testid="environment-release-preview"]').trigger('click')
  await flushPromises()
}
beforeEach(() => {
  current = receipt(); saved = []
  mocks.req.mockReset()
  mocks.req.mockImplementation(async (path: string, init?: RequestInit) => {
    if (path === base && !init?.method) return { items: structuredClone(saved) }
    if (path === base && init?.method === 'POST') { saved = [current]; return structuredClone(current) }
    if (path.endsWith('/confirm')) { current = receipt('confirmed'); saved = [current]; return structuredClone(current) }
    if (path.endsWith('/reconcile')) { current = receipt('released'); saved = [current]; return structuredClone(current) }
    if (path === `${base}/${current.id}`) return structuredClone(current)
    throw new Error(`Unexpected ${path}`)
  })
})
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount() })

describe('explicit environment release', () => {
  it('does not mutate on open, requires preview and separate permanent-loss acceptance', async () => {
    const wrapper = open()
    await flushPromises()
    expect(writes()).toHaveLength(0)
    await prepare(wrapper)
    expect(wrapper.text()).toContain('整个环境')
    expect(wrapper.text()).toContain('2 份已保存快照和 1 个返回点')
    expect(wrapper.text()).not.toContain(current.id)
    expect(wrapper.text()).not.toContain('generation')
    expect(wrapper.get('[data-testid="environment-release-confirm"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="environment-release-loss"]').setValue(true)
    await wrapper.get('[data-testid="environment-release-confirm"]').trigger('click')
    await flushPromises()
    const confirmations = writes().filter(([path]) => path.endsWith('/confirm'))
    expect(confirmations).toHaveLength(1)
    expect(JSON.parse(confirmations[0]![1].body)).toEqual({ clientRequestId: expect.any(String), confirmed: true, lossAccepted: true })
    expect(wrapper.text()).toContain('已受理释放')
    expect(wrapper.find('[data-testid="environment-release-confirm"]').exists()).toBe(false)
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(writes().filter(([path]) => path.endsWith('/reconcile'))).toHaveLength(0)
  })

  it('keeps the frozen preview nonce after a lost response', async () => {
    const standard = mocks.req.getMockImplementation()!
    const bodies: string[] = []
    mocks.req.mockImplementation(async (path: string, init?: RequestInit) => {
      if (path === base && init?.method === 'POST') { bodies.push(init.body as string); if (bodies.length === 1) throw new Error('offline') }
      return standard(path, init)
    })
    const wrapper = open()
    await prepare(wrapper)
    await wrapper.get('[data-testid="environment-release-preview"]').trigger('click')
    await flushPromises()
    expect(bodies).toHaveLength(2)
    expect(bodies[0]).toBe(bodies[1])
    expect(writes().filter(([path]) => path.endsWith('/confirm'))).toHaveLength(0)
  })

  it('retains a lost confirmation across drawer reopen and reconciles only on explicit click', async () => {
    const standard = mocks.req.getMockImplementation()!
    mocks.req.mockImplementation(async (path: string, init?: RequestInit) => {
      if (path.endsWith('/confirm')) { current = receipt('unknown'); saved = [current]; throw new Error('lost acknowledgement') }
      return standard(path, init)
    })
    const first = open(); await prepare(first)
    await first.get('[data-testid="environment-release-loss"]').setValue(true)
    await first.get('[data-testid="environment-release-confirm"]').trigger('click'); await flushPromises()
    const draft = held(first)
    expect(draft.uncertain).toBe(true)
    const confirmNonce = draft.confirmRequestId
    first.unmount()
    const second = open(draft); await flushPromises()
    expect(draft.confirmRequestId).toBe(confirmNonce)
    expect(second.find('[data-testid="environment-release-confirm"]').exists()).toBe(false)
    expect(writes().filter(([path]) => path.endsWith('/confirm'))).toHaveLength(1)
    expect(writes().filter(([path]) => path.endsWith('/reconcile'))).toHaveLength(0)
    await second.get('[data-testid="environment-release-check"]').trigger('click'); await flushPromises()
    expect(writes().filter(([path]) => path.endsWith('/confirm'))).toHaveLength(1)
    expect(writes().filter(([path]) => path.endsWith('/reconcile'))).toHaveLength(1)
    expect(second.text()).toContain('环境已释放')
    expect(second.emitted('changed')).toHaveLength(1)
  })

  it('recovers pending operations from server records after reload without confirming again', async () => {
    current = receipt('dispatching'); saved = [current]
    const wrapper = open(); await flushPromises()
    expect(wrapper.get('[data-testid="environment-release-check"]').exists()).toBe(true)
    expect(writes()).toHaveLength(0)
    await wrapper.get('[data-testid="environment-release-check"]').trigger('click'); await flushPromises()
    expect(writes().map(([path]) => path)).toEqual([`${base}/${current.id}/reconcile`])
  })

  it('reads a lost original confirm before allowing the same explicit nonce again', async () => {
    const standard = mocks.req.getMockImplementation()!
    let first = true
    mocks.req.mockImplementation(async (path: string, init?: RequestInit) => {
      if (path.endsWith('/confirm') && first) { first = false; throw new Error('request never accepted') }
      return standard(path, init)
    })
    const wrapper = open(); await prepare(wrapper)
    await wrapper.get('[data-testid="environment-release-loss"]').setValue(true)
    await wrapper.get('[data-testid="environment-release-confirm"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="environment-release-confirm"]').exists()).toBe(false)
    await wrapper.get('[data-testid="environment-release-check"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="environment-release-confirm"]').trigger('click'); await flushPromises()
    const confirms = writes().filter(([path]) => path.endsWith('/confirm'))
    expect(confirms).toHaveLength(2)
    expect(confirms[0]![1].body).toBe(confirms[1]![1].body)
    expect(writes().filter(([path]) => path.endsWith('/reconcile'))).toHaveLength(0)
  })

  it('does not carry another chat draft into this scope', async () => {
    const wrapper = open({ sessionId: 'someone-else', previewRequestId: 'old-preview', confirmRequestId: 'old-confirm', receipt: receipt('unknown'), uncertain: true })
    await flushPromises()
    expect(held(wrapper).sessionId).toBe('solo-1')
    expect(held(wrapper).receipt).toBeNull()
    expect(writes()).toHaveLength(0)
    expect(wrapper.find('[data-testid="environment-release-preview"]').exists()).toBe(true)
  })

  it('shows unsupported capability without exposing provider diagnostics or changing the environment', async () => {
    const standard = mocks.req.getMockImplementation()!
    mocks.req.mockImplementation(async (path: string, init?: RequestInit) => {
      if (path === base && init?.method === 'POST') throw Object.assign(new Error('secret internal host / token'), { code: 'workspace_release_unsupported' })
      return standard(path, init)
    })
    const wrapper = open(); await prepare(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toContain('不支持可核验的释放')
    expect(wrapper.text()).not.toContain('secret internal host')
    expect(wrapper.find('[data-testid="environment-release-confirm"]').exists()).toBe(false)
  })

  it('requires a fresh manual preview after selecting a released historical receipt', async () => {
    current = receipt('released'); saved = [current]
    const wrapper = open(); await flushPromises()
    await wrapper.get('[data-testid="environment-release-previous"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('环境已释放')
    await wrapper.get('[data-testid="environment-release-restart"]').trigger('click'); await flushPromises()
    expect(held(wrapper).previewRequestId).toBe('')
    expect(wrapper.get('[data-testid="environment-release-preview"]').exists()).toBe(true)
    expect(writes()).toHaveLength(0)
  })
})
