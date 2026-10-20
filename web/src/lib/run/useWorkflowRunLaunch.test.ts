// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createApp, defineComponent } from 'vue'
import common from '@/locales/zh-CN/common.json'
import { useWorkflowRunLaunch } from './useWorkflowRunLaunch'
import type { Workflow } from '@/lib/shared/types'
import { mergeRunDraft } from './runDraft'

vi.mock('@/lib/run/runDraft', () => ({
  mergeRunDraft: vi.fn(async (_id: string, seed: Record<string, string>, images: Record<string, unknown>) => ({
    inputs: seed,
    images,
    restored: false,
  })),
  saveRunDraft: vi.fn(async () => 'ok'),
  clearRunDraft: vi.fn(async () => {}),
}))

function withSetup<T>(fn: () => T): T {
  let result!: T
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': common },
  })
  const Comp = defineComponent({
    setup() {
      result = fn()
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return result
}

const baseWf = (over: Partial<Workflow> = {}): Workflow => ({
  id: 'wf-1',
  projectId: 'p1',
  name: '夜间回归',
  description: '',
  status: 'draft',
  version: 1,
  updatedAt: '',
  needsRepo: false,
  nodes: [
    {
      id: 'n1',
      type: 'input',
      label: '输入',
      config: {
        variables: [
          { name: 'branch', ask: true, type: 'string', value: 'main', required: true },
        ],
      },
    } as any,
  ],
  edges: [],
  ...over,
})

beforeEach(() => {
  const launch = withSetup(() => useWorkflowRunLaunch())
  launch.closeLaunch()
})

describe('useWorkflowRunLaunch', () => {
  it('openLaunch pre-fills ask fields and opens the modal without changing route', async () => {
    const launch = withSetup(() => useWorkflowRunLaunch())
    await launch.openLaunch(baseWf())
    expect(launch.open.value).toBe(true)
    expect(launch.target.value?.id).toBe('wf-1')
    expect(launch.runFields.value.map((f) => f.key)).toEqual(['branch'])
    expect(launch.runInputs.value.branch).toBe('main')
  })

  it('discards a pending launch after the shell closes it on account change', async () => {
    let resolve!: (value: any) => void
    vi.mocked(mergeRunDraft).mockReturnValueOnce(new Promise((done) => { resolve = done }))
    const launch = withSetup(() => useWorkflowRunLaunch())
    const pending = launch.openLaunch(baseWf())
    launch.closeLaunch()
    resolve({ inputs: { branch: 'private' }, images: {}, restored: true })
    await pending
    expect(launch.open.value).toBe(false)
    expect(launch.target.value).toBeNull()
    expect(launch.runInputs.value).toEqual({})
  })

  it('openLaunch still opens when there are no ask fields', async () => {
    const launch = withSetup(() => useWorkflowRunLaunch())
    await launch.openLaunch(
      baseWf({
        nodes: [{ id: 'n1', type: 'input', label: '输入', config: { variables: [] } } as any],
      }),
    )
    expect(launch.open.value).toBe(true)
    expect(launch.runFields.value).toEqual([])
  })
})
