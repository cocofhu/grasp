// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentWorkspaceHistoryPanel from '@/components/agent/AgentWorkspaceHistoryPanel.vue'
import { api } from '@/lib/api/api'

vi.mock('@/lib/api/api', () => ({
  api: {
    listAgentWorkspaceRevisions: vi.fn(),
    getAgentWorkspaceRevisionDiff: vi.fn(),
    restoreAgentWorkspaceRevision: vi.fn(),
  },
}))

const sampleRevisions = [
  {
    sha: '722d7da1111111',
    author: 'Test',
    source: 'system',
    reason: 'write AGENTS.md',
    createdAt: new Date().toISOString(),
    changes: [{ path: 'AGENTS.md', op: 'write' }],
  },
  {
    sha: 'b7227e12222222',
    author: 'Test',
    source: 'studio',
    reason: 'write rules/role.md',
    createdAt: new Date().toISOString(),
    changes: [{ path: 'rules/role.md', op: 'write' }],
  },
]

function mountPanel(props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const root = document.createElement('div')
  document.body.appendChild(root)
  const app = createApp(AgentWorkspaceHistoryPanel, {
    agentName: 'coder',
    isMobile: false,
    filePath: 'AGENTS.md',
    collapsed: false,
    ...props,
  })
  app.use(i18n)
  app.mount(root)
  return { app, root }
}

describe('AgentWorkspaceHistoryPanel', () => {
  beforeEach(() => {
    vi.mocked(api.listAgentWorkspaceRevisions).mockResolvedValue({ revisions: sampleRevisions })
    vi.mocked(api.getAgentWorkspaceRevisionDiff).mockResolvedValue({
      sha: '722d7da1111111',
      diff: '@@ -1 +1 @@\n-old\n+new',
    })
    vi.mocked(api.restoreAgentWorkspaceRevision).mockResolvedValue({ status: 'ok', sha: '722d7da1111111', agent: {} as never })
  })

  afterEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('filters revisions to the current file only', async () => {
    const { app, root } = mountPanel({ filePath: 'AGENTS.md' })
    await nextTick()
    await nextTick()
    const rows = root.querySelectorAll('[data-test="history-revision-row"]')
    expect(rows.length).toBe(1)
    expect(rows[0]?.getAttribute('data-sha')).toBe('722d7da1111111')
    app.unmount()
  })

  it('shows empty state when no file is open', async () => {
    const { app, root } = mountPanel({ filePath: '' })
    await nextTick()
    await nextTick()
    expect(root.querySelector('[data-test="history-empty-no-file"]')).toBeTruthy()
    app.unmount()
  })

  it('shows empty state when file has no revisions', async () => {
    const { app, root } = mountPanel({ filePath: 'notes.txt' })
    await nextTick()
    await nextTick()
    expect(root.querySelector('[data-test="history-empty-no-revisions"]')).toBeTruthy()
    app.unmount()
  })

  it('opens diff modal on row click and does not embed diff in sidebar', async () => {
    const { app, root } = mountPanel({ filePath: 'AGENTS.md' })
    await nextTick()
    await nextTick()
    expect(root.querySelector('[data-test="history-diff-body"]')).toBeFalsy()
    const row = root.querySelector('[data-test="history-revision-row"]') as HTMLButtonElement
    row.click()
    await nextTick()
    await nextTick()
    expect(api.getAgentWorkspaceRevisionDiff).toHaveBeenCalledWith('coder', '722d7da1111111')
    expect(document.querySelector('[data-test="history-diff-body"]')).toBeTruthy()
    app.unmount()
  })

  it('closes diff modal when file path changes', async () => {
    const filePath = ref('AGENTS.md')
    const Comp = defineComponent({
      components: { AgentWorkspaceHistoryPanel },
      setup() {
        return { filePath }
      },
      template: '<AgentWorkspaceHistoryPanel agent-name="coder" :file-path="filePath" :is-mobile="false" />',
    })
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const root = document.createElement('div')
    document.body.appendChild(root)
    const app = createApp(Comp)
    app.use(i18n)
    app.mount(root)
    await nextTick()
    await nextTick()
    const row = root.querySelector('[data-test="history-revision-row"]') as HTMLButtonElement
    row.click()
    await nextTick()
    await nextTick()
    expect(document.querySelector('[data-test="history-diff-body"]')).toBeTruthy()
    filePath.value = 'rules/role.md'
    await nextTick()
    await new Promise((r) => setTimeout(r, 250))
    expect(document.querySelector('[role="dialog"]')).toBeFalsy()
    app.unmount()
  })

  it('opens restore confirm from diff modal footer', async () => {
    const { app, root } = mountPanel({ filePath: 'AGENTS.md' })
    await nextTick()
    await nextTick()
    const row = root.querySelector('[data-test="history-revision-row"]') as HTMLButtonElement
    row.click()
    await nextTick()
    await nextTick()
    const restoreBtn = Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.includes('回滚到此版本'))
    restoreBtn?.click()
    await nextTick()
    expect(document.body.textContent).toContain('回滚到此版本？')
    app.unmount()
  })
})
