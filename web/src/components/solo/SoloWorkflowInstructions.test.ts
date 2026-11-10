// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import SoloWorkflowInstructions from './SoloWorkflowInstructions.vue'

const mocks = vi.hoisted(() => ({ req: vi.fn() }))
vi.mock('@/lib/api/httpCore', () => ({ req: mocks.req }))
const target = (generation = 'generation-a', state = 'open') => ({ runId: 'run-1', nodeId: 'impl', iteration: 2, generation, state })
const item = (id: string, sequence: number, state = 'queued', generation = 'generation-a') => ({ id, sequence, state, generation, iteration: 2, actorId: 'alice', text: `要求 ${id}` })
let history: { target: ReturnType<typeof target> | null; items: ReturnType<typeof item>[]; hasMore: boolean }
const wrappers: ReturnType<typeof mount>[] = []
function open(extra: { draft?: string; request?: { clientMessageId: string; text: string; generation: string; iteration: number } } = {}) {
  const wrapper = mount(SoloWorkflowInstructions, { props: { runId: 'run-1', nodeId: 'impl', label: '实现', ...extra }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })] } })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => {
  vi.useFakeTimers()
  mocks.req.mockReset()
  history = { target: target(), items: [], hasMore: false }
  mocks.req.mockImplementation(async (_path: string, init?: RequestInit) => {
    if (init?.method === 'POST') return item('accepted', 1)
    return structuredClone(history)
  })
})
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount(); vi.useRealTimers() })

describe('workflow additional instructions', () => {
  it('loads older history requested while a poll is in flight', async () => {
    history = { target: target(), items: [item('recent', 5)], hasMore: true }
    const wrapper = open()
    await flushPromises()
    let release!: () => void
    mocks.req.mockImplementationOnce(() => new Promise((resolve) => { release = () => resolve(structuredClone(history)) }))
    await vi.advanceTimersByTimeAsync(1500)
    await wrapper.get('[data-testid="instruction-older"]').trigger('click')
    release()
    await flushPromises()
    expect(mocks.req.mock.calls.map(([path]) => String(path)).some((path) => path.endsWith('?before=5'))).toBe(true)
  })

  it('does not poll while the tab is hidden', async () => {
    open()
    await flushPromises()
    const calls = mocks.req.mock.calls.length
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    await vi.advanceTimersByTimeAsync(4500)
    expect(mocks.req.mock.calls.length).toBe(calls)
    visibility.mockRestore()
  })

  it('requires a user click and pins the displayed iteration and generation', async () => {
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="instruction-input"]').setValue('补充失败路径')
    expect(mocks.req.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(0)
    await wrapper.get('[data-testid="instruction-send"]').trigger('click')
    await flushPromises()
    const [path, init] = mocks.req.mock.calls.find(([, init]) => init?.method === 'POST')!
    expect(path).toBe('/runs/run-1/nodes/impl/instructions')
    expect(JSON.parse(init.body)).toMatchObject({ text: '补充失败路径', iteration: 2, generation: 'generation-a' })
    expect(JSON.parse(init.body).clientMessageId).toBeTruthy()
    expect((wrapper.get('[data-testid="instruction-input"]').element as HTMLTextAreaElement).value).toBe('')
    expect(wrapper.text()).toContain('等待本轮处理')
  })

  it('retains the draft and the same request identity after an uncertain response', async () => {
    let attempts = 0
    const sent: Record<string, string>[] = []
    mocks.req.mockImplementation(async (_path: string, init?: RequestInit) => {
      if (!init?.method) return structuredClone(history)
      sent.push(JSON.parse(init.body as string))
      if (++attempts === 1) throw new Error('网络暂不可用')
      return item('accepted', 1)
    })
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="instruction-input"]').setValue('保留未提交文件')
    await wrapper.get('[data-testid="instruction-send"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('网络暂不可用')
    expect((wrapper.get('[data-testid="instruction-input"]').element as HTMLTextAreaElement).value).toBe('保留未提交文件')
    await wrapper.get('[data-testid="instruction-send"]').trigger('click')
    await flushPromises()
    expect(sent).toHaveLength(2)
    expect(sent[1]).toEqual(sent[0])
  })

  it('keeps a draft on an old target until the user explicitly selects the new execution', async () => {
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="instruction-input"]').setValue('尚未发送的要求')
    history.target = target('generation-b')
    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()
    expect(wrapper.text()).toContain('节点执行已更新')
    expect(wrapper.get('[data-testid="instruction-send"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="instruction-adopt"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-testid="instruction-input"]').element as HTMLTextAreaElement).value).toBe('尚未发送的要求')
    await wrapper.get('[data-testid="instruction-send"]').trigger('click')
    await flushPromises()
    const post = mocks.req.mock.calls.find(([, init]) => init?.method === 'POST')!
    expect(JSON.parse(post[1].body).generation).toBe('generation-b')
  })

  it('reuses a parent-retained request when the task drawer is reopened', async () => {
    const sent: Record<string, string>[] = []
    mocks.req.mockImplementation(async (_path: string, init?: RequestInit) => {
      if (!init?.method) return structuredClone(history)
      sent.push(JSON.parse(init.body as string))
      if (sent.length === 1) throw new Error('response lost')
      return item('accepted', 1)
    })
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="instruction-input"]').setValue('保留此草稿和提交身份')
    await wrapper.get('[data-testid="instruction-send"]').trigger('click')
    await flushPromises()
    const request = wrapper.emitted('update:request')![0]![0] as { clientMessageId: string; text: string; generation: string; iteration: number }
    wrapper.unmount()
    const reopened = open({ draft: '保留此草稿和提交身份', request })
    await flushPromises()
    await reopened.get('[data-testid="instruction-send"]').trigger('click')
    await flushPromises()
    expect(sent).toHaveLength(2)
    expect(sent[1]).toEqual(sent[0])
  })

  it('shows unknown and expired results after closing without automatically resending them', async () => {
    history = { target: target('generation-b', 'sealed'), items: [item('old-unknown', 1, 'unknown'), item('old-expired', 2, 'expired')], hasMore: false }
    const wrapper = open()
    await flushPromises()
    expect(wrapper.text()).toContain('处理结果待核对')
    expect(wrapper.text()).toContain('本轮已过期')
    expect(wrapper.text()).toContain('本轮已结束')
    await wrapper.get('[data-testid="instruction-input"]').setValue('later draft')
    expect(wrapper.get('[data-testid="instruction-send"]').attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(3000)
    expect(mocks.req.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(0)
  })

  it('enforces the byte limit for Chinese instructions', async () => {
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="instruction-input"]').setValue('中'.repeat(6000))
    expect(wrapper.text()).toContain('不能超过 16 KB')
    expect(wrapper.get('[data-testid="instruction-send"]').attributes('disabled')).toBeDefined()
  })

  it('loads older receipts without losing them on the next poll', async () => {
    history = { target: target(), items: [item('new', 51)], hasMore: true }
    mocks.req.mockImplementation(async (path: string) => path.includes('before=51') ? { target: target(), items: [item('old', 1, 'unknown')], hasMore: false } : structuredClone(history))
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="instruction-older"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1500)
    expect(wrapper.find('[data-testid="instruction-old"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="instruction-new"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="instruction-older"]').exists()).toBe(false)
  })
})
