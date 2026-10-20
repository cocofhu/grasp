// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const importWorkflow = vi.fn()
const listAgents = vi.fn()
const push = vi.fn()

const auth = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  return { user: ref<{ username: string } | null>({ username: 'A' }) }
})
vi.mock('@/lib/composables/useAuth', () => ({ useAuth: () => auth }))

vi.mock('@/lib/api/api', () => ({
  api: {
    importWorkflow: (...args: unknown[]) => importWorkflow(...args),
    listAgents: (...args: unknown[]) => listAgents(...args),
  },
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
}))

vi.mock('@/lib/run/workflowIO', () => ({
  agentProfileIssues: () => [{ name: 'missing-agent', reason: 'missing' }],
}))

vi.mock('@/lib/composables/useWorkspaceContext', async () => {
  const { ref } = await import('vue')
  return { useWorkspaceContext: () => ({ workspaceId: ref('p1'), ensureLoaded: async () => ({ id: 'p1' }) }) }
})

import { useWorkflowImport } from './useWorkflowImport'

describe('useWorkflowImport', () => {
  beforeEach(() => {
    auth.user.value = { username: 'A' }
    importWorkflow.mockReset()
    listAgents.mockReset()
    push.mockReset()
    importWorkflow.mockResolvedValue({ id: 'w1', name: 'wf', nodes: [] })
    listAgents.mockResolvedValue([{ name: 'a1' }])
  })

  it('discards file content and late API results after an account change', async () => {
    let importer!: ReturnType<typeof useWorkflowImport>
    mount(defineComponent({ setup() { importer = useWorkflowImport(); return () => h('div') } }), { global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })] } })
    let read!: (value: string) => void
    const input = document.createElement('input')
    Object.defineProperty(input, 'files', { value: [{ text: () => new Promise<string>((resolve) => { read = resolve }) }], configurable: true })
    const reading = importer.handleFileChange({ target: input } as unknown as Event)
    auth.user.value = { username: 'B' }
    read('{}')
    await reading
    expect(importWorkflow).not.toHaveBeenCalled()

    let imported!: (value: any) => void
    importWorkflow.mockReturnValueOnce(new Promise((resolve) => { imported = resolve }))
    Object.defineProperty(input, 'files', { value: [new File(['{}'], 'wf.json')], configurable: true })
    const pending = importer.handleFileChange({ target: input } as unknown as Event)
    await vi.waitFor(() => expect(importWorkflow).toHaveBeenCalledTimes(1))
    auth.user.value = { username: 'C' }
    imported({ id: 'old-result', name: 'A data', nodes: [] })
    await pending
    expect(push).not.toHaveBeenCalled()
    expect(listAgents).not.toHaveBeenCalled()
  })

  it('imports workflow file and navigates', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    let api: ReturnType<typeof useWorkflowImport> | null = null
    mount(
      defineComponent({
        setup() {
          api = useWorkflowImport({ dirty: () => true })
          return () => h('div')
        },
      }),
      { global: { plugins: [i18n] } },
    )
    api!.triggerImport()
    expect(api!.showDiscardConfirm.value).toBe(true)
    api!.onDiscardCancel()
    api!.onDiscardConfirm()

    mount(
      defineComponent({
        setup() {
          api = useWorkflowImport()
          return () => h('div')
        },
      }),
      { global: { plugins: [i18n] } },
    )
    const input = document.createElement('input')
    api!.fileInput.value = input
    const file = new File(['{}'], 'wf.json', { type: 'application/json' })
    Object.defineProperty(input, 'files', { value: [file] })
    await api!.handleFileChange({ target: input } as unknown as Event)
    expect(importWorkflow).toHaveBeenCalled()
    expect(push).toHaveBeenCalledWith('/workflows/w1/edit')
  })
})
