// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const importAgent = vi.fn()
const importProjectAgents = vi.fn()
const peekZipPackage = vi.fn()

vi.mock('@/lib/api/api', () => ({
  api: {
    importAgent: (...args: unknown[]) => importAgent(...args),
    importProjectAgents: (...args: unknown[]) => importProjectAgents(...args),
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

async function pickFile(api: ReturnType<typeof useAgentImport>, projectId: string, name: string) {
  const input = document.createElement('input')
  input.click = vi.fn()
  api.fileInput.value = input
  api.triggerImport(projectId)
  const file = new File(['z'], name, { type: 'application/zip' })
  Object.defineProperty(input, 'files', { value: [file] })
  await api.handleFileChange({ target: input } as unknown as Event)
  return { file, input }
}

describe('useAgentImport', () => {
  beforeEach(() => {
    importAgent.mockReset()
    importProjectAgents.mockReset()
    peekZipPackage.mockReset()
    importAgent.mockResolvedValue({ name: 'agent-a', projectId: 'p1' })
    importProjectAgents.mockResolvedValue({ created: ['bob', 'carol'] })
  })

  it('asks to discard a dirty draft before opening the picker', () => {
    const api = mountHook({ dirty: () => true })
    const input = document.createElement('input')
    input.click = vi.fn()
    api.fileInput.value = input
    api.triggerImport('p1')
    expect(api.showDiscardConfirm.value).toBe(true)
    expect(input.click).not.toHaveBeenCalled()
    api.onDiscardCancel()
    expect(api.showDiscardConfirm.value).toBe(false)
  })

  it('ignores triggers without a target project', () => {
    const api = mountHook()
    const input = document.createElement('input')
    input.click = vi.fn()
    api.fileInput.value = input
    api.triggerImport('  ')
    expect(input.click).not.toHaveBeenCalled()
  })

  it('imports a single-agent zip into the target project and resolves name conflicts', async () => {
    peekZipPackage.mockResolvedValue({ kind: 'agent', name: 'agent-a' })
    const onImported = vi.fn()
    const api = mountHook({ onImported })
    const { file } = await pickFile(api, 'p1', 'agent-a.zip')
    expect(api.showConflict.value).toBe(true)
    api.selectConflict('overwrite')
    await api.confirmConflict()
    expect(importAgent).toHaveBeenCalledWith(file, { projectId: 'p1', targetName: 'agent-a', mode: 'overwrite' })
    expect(onImported).toHaveBeenCalled()
    expect(importProjectAgents).not.toHaveBeenCalled()
  })

  it('imports a project bundle into the target project', async () => {
    peekZipPackage.mockResolvedValue({ kind: 'project-bundle', agentNames: ['bob', 'carol'] })
    const onBundleImported = vi.fn()
    const api = mountHook({ onBundleImported })
    const { file } = await pickFile(api, 'p2', 'demo-agents.zip')
    expect(api.showBatchConflict.value).toBe(false)
    expect(importProjectAgents).toHaveBeenCalledWith('p2', file, { mode: 'rename' })
    expect(onBundleImported).toHaveBeenCalledWith({ created: ['bob', 'carol'] })
    expect(importAgent).not.toHaveBeenCalled()
  })

  it('shows one batch dialog for bundle name conflicts', async () => {
    peekZipPackage.mockResolvedValue({ kind: 'project-bundle', agentNames: ['agent-a', 'bob'] })
    const api = mountHook({ agentNames: () => ['agent-a', 'other'] })
    const { file } = await pickFile(api, 'p1', 'demo-agents.zip')
    expect(api.showBatchConflict.value).toBe(true)
    expect(api.batchConflictNames.value).toEqual(['agent-a'])
    expect(importProjectAgents).not.toHaveBeenCalled()

    await api.confirmBatchOverwrite()
    expect(importProjectAgents).toHaveBeenCalledWith('p1', file, { mode: 'overwrite' })
  })

  it('surfaces bundle import failures as a rolled-back error', async () => {
    peekZipPackage.mockResolvedValue({ kind: 'project-bundle', agentNames: ['bob'] })
    importProjectAgents.mockRejectedValueOnce(new Error('project.json kind 无效'))
    const api = mountHook({ agentNames: () => [] })
    await pickFile(api, 'p1', 'demo-agents.zip')
    expect(api.showImportError.value).toBe(true)
    expect(api.importError.value).toContain(pages.pages.agentStudio.exportImport.importError.rolledBack)
    expect(api.importError.value).toContain('project.json kind 无效')
  })

  it('unrecognized zip shows visible error and does not write', async () => {
    peekZipPackage.mockResolvedValue({ kind: 'unknown', error: 'unrecognized' })
    const api = mountHook()
    await pickFile(api, 'p1', 'mystery.zip')
    expect(api.showImportError.value).toBe(true)
    expect(api.importError.value).toMatch(/无法识别/)
    expect(importAgent).not.toHaveBeenCalled()
    expect(importProjectAgents).not.toHaveBeenCalled()
  })
})
