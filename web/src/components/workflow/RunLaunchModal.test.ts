// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

function findStartButton(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll('button').find((b) => b.text().includes('开始运行') || b.text().match(/Start/i))
}

const apiMocks = vi.hoisted(() => ({
  startRun: vi.fn(),
  listRepos: vi.fn(),
  listRunTags: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      startRun: apiMocks.startRun,
      listRepos: apiMocks.listRepos,
      listRunTags: apiMocks.listRunTags,
    },
  }
})

const routerPush = vi.hoisted(() => vi.fn())
vi.mock('vue-router', () => ({ useRouter: () => ({ push: routerPush }) }))

import RunLaunchModal from './RunLaunchModal.vue'
import { takeHomeApproveHandoff } from '@/lib/run/homeApproveHandoff'

function mountModal(open = true, extraProps: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(RunLaunchModal, {
    props: {
      open,
      workflowId: 'wf-1',
      workflowName: '测试工作流',
      fields: [{ key: 'topic', desc: '主题', required: true }],
      runInputs: { topic: 'hello' },
      runImages: {},
      ...extraProps,
    },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        AppButton: { template: '<button v-bind="$attrs"><slot /></button>' },
        AppModal: {
          props: ['open', 'title'],
          emits: ['close'],
          template:
            '<div v-if="open" data-testid="modal"><button data-testid="modal-close" @click="$emit(\'close\')" /><slot name="header" /><slot /><slot name="footer" /></div>',
        },
        ParagraphInput: {
          props: ['text'],
          emits: ['update:text'],
          template: '<textarea data-testid="paragraph" :value="text" @input="$emit(\'update:text\', $event.target.value)" />',
        },
        HardLoadLayer: true,
        ReposEditor: true,
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.listRepos.mockResolvedValue([])
  apiMocks.listRunTags.mockResolvedValue({ tags: [] })
})

describe('RunLaunchModal', () => {
  it('shows form when open with workflow name', async () => {
    const wrapper = mountModal(true)
    await flushPromises()
    expect(wrapper.find('[data-testid="modal"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('测试工作流')
    wrapper.unmount()
  })

  it('does not render body when closed', () => {
    const wrapper = mountModal(false)
    expect(wrapper.find('[data-testid="modal"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows required validation error when topic empty', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(RunLaunchModal, {
      props: {
        open: true,
        workflowId: 'wf-1',
        workflowName: '测试工作流',
        fields: [{ key: 'topic', desc: '主题', required: true }],
        runInputs: { topic: '' },
        runImages: {},
      },
      global: {
        plugins: [i18n],
        stubs: {
          Icon: true,
          AppButton: { template: '<button v-bind="$attrs"><slot /></button>' },
          AppModal: { props: ['open'], template: '<div v-if="open"><slot /><slot name="footer" /></div>' },
          ParagraphInput: true,
          HardLoadLayer: true,
          ReposEditor: true,
          PrioritySegmented: true,
        },
      },
    })
    await flushPromises()
    const startBtn = findStartButton(wrapper)
    expect(startBtn).toBeTruthy()
    await startBtn!.trigger('click')
    await flushPromises()
    expect(apiMocks.startRun).not.toHaveBeenCalled()
    expect(wrapper.text()).toMatch(/主题|topic|必填|required/i)
    wrapper.unmount()
  })

  it('calls startRun and switches to success phase', async () => {
    apiMocks.startRun.mockResolvedValue({ id: 'run-99' })
    const wrapper = mountModal(true)
    await flushPromises()
    const startBtn = findStartButton(wrapper)
    await startBtn!.trigger('click')
    await flushPromises()
    expect(apiMocks.startRun).toHaveBeenCalledWith(
      'wf-1',
      expect.objectContaining({ topic: 'hello' }),
      'manual',
      'normal',
      [],
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    expect(apiMocks.startRun.mock.calls[0]?.[5]).not.toHaveProperty('publishedSnapshot')
    expect(wrapper.emitted('started')?.[0]?.[0]).toBe('run-99')
    // plan g2.1: success phase first — no auto view-run/close
    expect(wrapper.emitted('view-run')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeFalsy()
    expect(wrapper.text()).toMatch(/工作流已启动|Workflow started/)
    expect(wrapper.text()).toMatch(/查看运行|View run/)
    expect(wrapper.text()).toMatch(/留在当前页|Stay on this page/)
    wrapper.unmount()
  })

  it('sends publishedSnapshot when opened from the home page', async () => {
    apiMocks.startRun.mockResolvedValue({ id: 'run-pub' })
    const wrapper = mountModal(true, { publishedSnapshot: true })
    await flushPromises()
    const startBtn = findStartButton(wrapper)
    await startBtn!.trigger('click')
    await flushPromises()
    expect(apiMocks.startRun).toHaveBeenCalledWith(
      'wf-1',
      expect.anything(),
      'manual',
      'normal',
      [],
      expect.objectContaining({ publishedSnapshot: true }),
    )
    expect(wrapper.emitted('started')?.[0]?.[0]).toBe('run-pub')
    wrapper.unmount()
  })

  it('passes runTitle through to startRun', async () => {
    apiMocks.startRun.mockResolvedValue({ id: 'run-title' })
    const wrapper = mountModal(true, { runTitle: '  用户第一句话  ' })
    await flushPromises()
    const startBtn = findStartButton(wrapper)
    await startBtn!.trigger('click')
    await flushPromises()
    expect(apiMocks.startRun).toHaveBeenCalledWith(
      'wf-1',
      expect.anything(),
      'manual',
      'normal',
      [],
      expect.objectContaining({ title: '用户第一句话' }),
    )
    expect(wrapper.emitted('started')?.[0]?.[0]).toBe('run-title')
    wrapper.unmount()
  })

  it('rejects reserved env keys in modal without calling API', async () => {
    const wrapper = mountModal(true)
    await flushPromises()
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('添加行'))
    expect(addBtn).toBeTruthy()
    await addBtn!.trigger('click')
    await flushPromises()
    const row = wrapper.find('[data-testid="run-launch-env-row"]')
    const inputs = row.findAll('input')
    await inputs[0].setValue('CURSOR_API_KEY')
    await inputs[1].setValue('x')
    const startBtn = findStartButton(wrapper)
    await startBtn!.trigger('click')
    await flushPromises()
    expect(apiMocks.startRun).not.toHaveBeenCalled()
    expect(wrapper.text()).toMatch(/CURSOR_API_KEY/)
    wrapper.unmount()
  })

  it('passes env entries to startRun when valid', async () => {
    apiMocks.startRun.mockResolvedValue({ id: 'run-env' })
    const wrapper = mountModal(true)
    await flushPromises()
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('添加行'))
    await addBtn!.trigger('click')
    await flushPromises()
    const row = wrapper.find('[data-testid="run-launch-env-row"]')
    const inputs = row.findAll('input')
    await inputs[0].setValue('LOG_LEVEL')
    await inputs[1].setValue('debug')
    const startBtn = findStartButton(wrapper)
    await startBtn!.trigger('click')
    await flushPromises()
    expect(apiMocks.startRun).toHaveBeenCalledWith(
      'wf-1',
      expect.anything(),
      'manual',
      'normal',
      [],
      expect.objectContaining({
        env: [{ key: 'LOG_LEVEL', value: 'debug', secret: false }],
      }),
    )
    // plan g1.3 / g2.1: sandbox env start still reaches success phase (no auto navigate)
    expect(wrapper.emitted('view-run')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeFalsy()
    expect(wrapper.text()).toMatch(/工作流已启动|Workflow started/)
    expect(wrapper.text()).toMatch(/查看运行|View run/)
    wrapper.unmount()
  })

  it('shows error phase when startRun rejects', async () => {
    apiMocks.startRun.mockRejectedValue(new Error('network down'))
    const wrapper = mountModal(true)
    await flushPromises()
    const startBtn = findStartButton(wrapper)
    await startBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('network down')
    expect(apiMocks.startRun).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('fetches workspace run-tags when mounted with open=true (v-if path)', async () => {
    apiMocks.listRunTags.mockResolvedValue({ tags: ['bugfix-login', 'spike'] })
    const wrapper = mountModal(true)
    await flushPromises()
    expect(apiMocks.listRunTags).toHaveBeenCalledTimes(1)
    expect(apiMocks.listRunTags).toHaveBeenCalledWith()
    wrapper.unmount()
  })

  it('fetches workspace run-tags when open flips false→true (editor path)', async () => {
    apiMocks.listRunTags.mockResolvedValue({ tags: ['bugfix'] })
    const wrapper = mountModal(false)
    await flushPromises()
    expect(apiMocks.listRunTags).not.toHaveBeenCalled()
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(apiMocks.listRunTags).toHaveBeenCalledTimes(1)
    expect(apiMocks.listRunTags).toHaveBeenCalledWith()
    wrapper.unmount()
  })

  it('aborts in-flight start when modal closes during loading', async () => {
    let resolveStart!: (v: { id: string }) => void
    apiMocks.startRun.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveStart = resolve
        }),
    )
    const wrapper = mountModal(true)
    await flushPromises()
    const startBtn = findStartButton(wrapper)
    await startBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/启动中|Starting/i)
    await wrapper.get('[data-testid="modal-close"]').trigger('click')
    resolveStart({ id: 'run-late' })
    await flushPromises()
    expect(wrapper.emitted('started')).toBeFalsy()
    wrapper.unmount()
  })

  it('keeps empty tag suggestions when listRunTags rejects', async () => {
    apiMocks.listRunTags.mockRejectedValue(new Error('not found'))
    const wrapper = mountModal(true)
    await flushPromises()
    expect(apiMocks.listRunTags).toHaveBeenCalledWith()
    expect(wrapper.text()).not.toMatch(/is not iterable/i)
    wrapper.unmount()
  })

  it('prefills segmented priority from initialPriority (plan g2.2 / g3.4)', async () => {
    const wrapper = mountModal(true, { initialPriority: 'high' })
    await flushPromises()
    const checked = wrapper.findAll('[role="radio"]').find((b) => b.attributes('aria-checked') === 'true')
    expect(checked?.text()).toBe('高')
    wrapper.unmount()
  })

  it('defaults to normal when initialPriority is omitted or invalid (plan g3.4)', async () => {
    const omitted = mountModal(true)
    await flushPromises()
    const omittedChecked = omitted.findAll('[role="radio"]').find((b) => b.attributes('aria-checked') === 'true')
    expect(omittedChecked?.text()).toBe('普通')
    omitted.unmount()

    const invalid = mountModal(true, { initialPriority: 'urgent' })
    await flushPromises()
    const invalidChecked = invalid.findAll('[role="radio"]').find((b) => b.attributes('aria-checked') === 'true')
    expect(invalidChecked?.text()).toBe('普通')
    invalid.unmount()
  })

  it('starts with the modal priority after the user changes it (plan g2.2)', async () => {
    apiMocks.startRun.mockResolvedValue({ id: 'run-prio' })
    const wrapper = mountModal(true, { initialPriority: 'high' })
    await flushPromises()
    const lowBtn = wrapper.findAll('[role="radio"]').find((b) => b.text() === '低')
    await lowBtn!.trigger('click')
    const startBtn = findStartButton(wrapper)
    await startBtn!.trigger('click')
    await flushPromises()
    expect(apiMocks.startRun).toHaveBeenCalledWith(
      'wf-1',
      expect.objectContaining({ topic: 'hello' }),
      'manual',
      'low',
      [],
      expect.anything(),
    )
    wrapper.unmount()
  })

  it('hides the message box when the caller already passes a first message', async () => {
    const wrapper = mountModal(true, { firstMessage: { text: 'from home' } })
    await flushPromises()
    expect(wrapper.find('[data-testid="run-launch-message"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('starts without title or first message when the message box is empty', async () => {
    apiMocks.startRun.mockResolvedValue({ id: 'run-empty' })
    const wrapper = mountModal(true)
    await flushPromises()
    expect(wrapper.find('[data-testid="run-launch-message"]').exists()).toBe(true)
    await findStartButton(wrapper)!.trigger('click')
    await flushPromises()
    const opts = apiMocks.startRun.mock.calls[0][5]
    expect(opts.title).toBeUndefined()
    expect(opts.firstMessage).toBeUndefined()
    expect(routerPush).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('sends the typed message as title + first message and opens the inbox chat', async () => {
    apiMocks.startRun.mockResolvedValue({ id: 'run-msg' })
    const wrapper = mountModal(true)
    await flushPromises()
    await wrapper.find('[data-testid="run-launch-message"] [data-testid="paragraph"]').setValue('  修一下登录页  ')
    await findStartButton(wrapper)!.trigger('click')
    await flushPromises()
    const opts = apiMocks.startRun.mock.calls[0][5]
    expect(opts.title).toBe('修一下登录页')
    expect(opts.firstMessage).toEqual({ text: '修一下登录页', images: [] })
    expect(takeHomeApproveHandoff()).toMatchObject({ runId: 'run-msg', text: '修一下登录页' })
    expect(routerPush).toHaveBeenCalledWith({ path: '/runs/run-msg/nodes/incoming' })
    expect(wrapper.emitted('close')).toBeTruthy()
    wrapper.unmount()
  })
})
