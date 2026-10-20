// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const importAgent = vi.fn()
const importWorkspaceAgents = vi.fn()
const peekZipPackage = vi.fn()

vi.mock('@/lib/api/api', () => ({
  api: {
    importAgent: (...args: unknown[]) => importAgent(...args),
    importWorkspaceAgents: (...args: unknown[]) => importWorkspaceAgents(...args),
  },
}))

vi.mock('@/lib/agent/agentIO', () => ({
  peekZipPackage: (...args: unknown[]) => peekZipPackage(...args),
  peekAgentZipName: vi.fn(async () => ({ name: 'agent-a' })),
  resolveImportName: (name: string) => name,
  suggestRename: (name: string) => `${name}_v2`,
  normalizeAgentName: (name: string) => name,
  validateAgentName: () => '',
}))

import { useAgentImport } from './useAgentImport'

function mountHook(opts: Partial<Parameters<typeof useAgentImport>[0]> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  let api: ReturnType<typeof useAgentImport> | null = null
  mount(
    defineComponent({
      setup() {
        api = useAgentImport({
          dirty: () => false,
          agentNames: () => ['agent-a'],
          onImported: vi.fn(),
          onBundleImported: vi.fn(),
          ...opts,
        })
        return () => h('div')
      },
    }),
    { global: { plugins: [i18n] } },
  )
  return api!
}

async function pickFile(api: ReturnType<typeof useAgentImport>, name: string) {
  const input = document.createElement('input')
  input.click = vi.fn()
  api.fileInput.value = input
  api.triggerImport()
  expect(input.click).toHaveBeenCalledTimes(1)
  const file = new File(['z'], name, { type: 'application/zip' })
  Object.defineProperty(input, 'files', { value: [file] })
  await api.handleFileChange({ target: input } as unknown as Event)
  return { file, input }
}

describe('useAgentImport', () => {
  beforeEach(() => {
    importAgent.mockReset()
    importWorkspaceAgents.mockReset()
    peekZipPackage.mockReset()
    importAgent.mockResolvedValue({ name: 'agent-a' })
    importWorkspaceAgents.mockResolvedValue({ created: ['bob', 'carol'] })
  })

  it('asks to discard a dirty draft before opening the picker', () => {
    const api = mountHook({ dirty: () => true })
    const input = document.createElement('input')
    input.click = vi.fn()
    api.fileInput.value = input
    api.triggerImport()
    expect(api.showDiscardConfirm.value).toBe(true)
    expect(input.click).not.toHaveBeenCalled()
    api.onDiscardCancel()
    expect(api.showDiscardConfirm.value).toBe(false)
  })

  it('imports a single-agent zip into the workspace and resolves name conflicts', async () => {
    peekZipPackage.mockResolvedValue({ kind: 'agent', name: 'agent-a' })
    const onImported = vi.fn()
    const api = mountHook({ onImported })
    const { file } = await pickFile(api, 'agent-a.zip')
    expect(api.showConflict.value).toBe(true)
    api.selectConflict('overwrite')
    await api.confirmConflict()
    expect(importAgent).toHaveBeenCalledWith(file, { targetName: 'agent-a', mode: 'overwrite' })
    expect(onImported).toHaveBeenCalled()
    expect(importWorkspaceAgents).not.toHaveBeenCalled()
  })

  it('imports a workspace bundle', async () => {
    peekZipPackage.mockResolvedValue({ kind: 'workspace-bundle', agentNames: ['bob', 'carol'] })
    const onBundleImported = vi.fn()
    const api = mountHook({ onBundleImported })
    const { file } = await pickFile(api, 'demo-agents.zip')
    expect(api.showBatchConflict.value).toBe(false)
    expect(importWorkspaceAgents).toHaveBeenCalledWith(file, { mode: 'rename' })
    expect(onBundleImported).toHaveBeenCalledWith({ created: ['bob', 'carol'] })
    expect(importAgent).not.toHaveBeenCalled()
  })

  it('shows one batch dialog for bundle name conflicts', async () => {
    peekZipPackage.mockResolvedValue({ kind: 'workspace-bundle', agentNames: ['agent-a', 'bob'] })
    const api = mountHook({ agentNames: () => ['agent-a', 'other'] })
    const { file } = await pickFile(api, 'demo-agents.zip')
    expect(api.showBatchConflict.value).toBe(true)
    expect(api.batchConflictNames.value).toEqual(['agent-a'])
    expect(importWorkspaceAgents).not.toHaveBeenCalled()

    await api.confirmBatchOverwrite()
    expect(importWorkspaceAgents).toHaveBeenCalledWith(file, { mode: 'overwrite' })
  })

  it('surfaces bundle import failures as a rolled-back error', async () => {
    peekZipPackage.mockResolvedValue({ kind: 'workspace-bundle', agentNames: ['bob'] })
    importWorkspaceAgents.mockRejectedValueOnce(new Error('workspace.json kind 无效'))
    const api = mountHook({ agentNames: () => [] })
    await pickFile(api, 'demo-agents.zip')
    expect(api.showImportError.value).toBe(true)
    expect(api.importError.value).toContain(pages.pages.agentStudio.exportImport.importError.rolledBack)
    expect(api.importError.value).toContain('workspace.json kind 无效')
  })

  it('unrecognized zip shows visible error and does not write', async () => {
    peekZipPackage.mockResolvedValue({ kind: 'unknown', error: 'unrecognized' })
    const api = mountHook()
    await pickFile(api, 'mystery.zip')
    expect(api.showImportError.value).toBe(true)
    expect(api.importError.value).toMatch(/无法识别/)
    expect(importAgent).not.toHaveBeenCalled()
    expect(importWorkspaceAgents).not.toHaveBeenCalled()
  })
})
