// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createApp, defineComponent, nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { AGENT_SETTINGS_PATH } from '@/lib/agent/agentCreateWizard'
import type { AgentStudioDraft, DraftFile } from '@/lib/agent/agentStudioDraft'
import { useAgentFilesPanel } from './useAgentFilesPanel'

const baseDraft = (): AgentStudioDraft => ({
  name: 'agent-a',
  projectId: 'proj-1',
  acpBackend: 'cursor',
  files: [
    { path: 'README.md', content: '# hello' },
    { path: 'src/main.ts', content: 'export {}' },
    { path: 'rules/base.md', content: 'rule' },
  ],
  mcp: [],
  env: [],
  layout: { configRoot: '/root/.cursor', workspaceDir: '/root/workspace' },
  capabilities: null,
})

function withFilesPanel(over: { isMobile?: boolean; dirty?: boolean; agentName?: string } = {}) {
  let panel!: ReturnType<typeof useAgentFilesPanel>
  const emit = vi.fn()
  const save = vi.fn(async () => true)
  const props = reactive({
    draft: baseDraft(),
    dirty: over.dirty ?? false,
    isMobile: over.isMobile ?? false,
    agentName: 'agentName' in over ? over.agentName : 'agent-a',
    save,
  })
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const Comp = defineComponent({
    setup() {
      panel = useAgentFilesPanel(props as never, emit as never)
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return { panel, app, emit, props, save }
}

function mouseEvt(clientX = 100, clientY = 100, currentTarget?: HTMLElement) {
  return {
    clientX,
    clientY,
    currentTarget: currentTarget || document.createElement('div'),
    preventDefault: vi.fn(),
    stopPropagation: vi.fn(),
  } as unknown as MouseEvent
}

function row(path: string, dir = false) {
  return { name: path.split('/').pop() || path, path, dir, depth: 0 }
}

describe('useAgentFilesPanel explorer', () => {
  beforeEach(() => {
    localStorage.clear()
    Object.defineProperty(window, 'innerWidth', { value: 900, configurable: true })
    Object.defineProperty(window, 'innerHeight', { value: 700, configurable: true })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('opens the context menu and clamps it inside the viewport', async () => {
    const { panel, app } = withFilesPanel()
    const menu = document.createElement('div')
    menu.className = 'explorer-ctx-menu'
    menu.getBoundingClientRect = () =>
      ({ width: 200, height: 300, top: 0, left: 0, right: 0, bottom: 0, x: 0, y: 0 }) as DOMRect
    document.body.appendChild(menu)

    panel.openCtxMenu(mouseEvt(880, 690), { dir: false, path: 'README.md', name: 'README.md' })
    expect(panel.ctxMenu.value.open).toBe(true)
    expect(panel.selectedTreeRow.value?.path).toBe('README.md')
    await nextTick()
    expect(panel.ctxMenu.value.x).toBe(900 - 200 - 8)
    expect(panel.ctxMenu.value.y).toBe(700 - 300 - 8)

    // Directory / blank targets clear the row selection.
    panel.openCtxMenu(mouseEvt(10, 10), { dir: true, path: 'src', name: 'src' })
    expect(panel.selectedTreeRow.value).toBeNull()
    panel.onExplorerBlankCtx(mouseEvt(10, 10))
    expect(panel.ctxMenu.value.target?.blank).toBe(true)

    // Without a rendered menu the click coordinates stand.
    menu.remove()
    panel.openCtxMenu(mouseEvt(42, 43), { dir: false, path: 'README.md', name: 'README.md' })
    await nextTick()
    expect(panel.ctxMenu.value.x).toBe(42)

    panel.hideCtxMenu()
    expect(panel.ctxMenu.value.open).toBe(false)

    app.unmount()
  })

  it('routes every explorer context action', async () => {
    const { panel, app, emit, props } = withFilesPanel()
    const writeText = vi.fn(() => Promise.resolve())
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })

    // No target → nothing happens.
    panel.onCtxAction('newFile')

    panel.openCtxMenu(mouseEvt(), { dir: true, path: 'src', name: 'src', blank: true })
    panel.onCtxAction('newFile')
    expect(panel.creating.value).toEqual({ dir: '', kind: 'file' })
    panel.cancelCreate()

    panel.openCtxMenu(mouseEvt(), { dir: true, path: 'src', name: 'src' })
    panel.onCtxAction('newFolder')
    expect(panel.creating.value).toEqual({ dir: 'src', kind: 'folder' })
    panel.cancelCreate()

    panel.openCtxMenu(mouseEvt(), { dir: false, path: 'src/main.ts', name: 'main.ts' })
    panel.onCtxAction('newFile')
    expect(panel.creating.value).toEqual({ dir: 'src', kind: 'file' })
    panel.cancelCreate()
    panel.openCtxMenu(mouseEvt(), { dir: false, path: 'README.md', name: 'README.md' })
    panel.onCtxAction('newFolder')
    expect(panel.creating.value).toEqual({ dir: '', kind: 'folder' })
    panel.cancelCreate()

    // Folder upload only applies to a real directory.
    const click = vi.fn()
    panel.folderInput.value = { click } as unknown as HTMLInputElement
    panel.openCtxMenu(mouseEvt(), { dir: false, path: 'README.md', name: 'README.md' })
    panel.onCtxAction('uploadFolder')
    expect(click).not.toHaveBeenCalled()
    panel.openCtxMenu(mouseEvt(), { dir: true, path: 'src', name: 'src' })
    panel.onCtxAction('uploadFolder')
    expect(click).toHaveBeenCalled()
    expect(panel.uploadTargetDir.value).toBe('src')

    // Rename is blocked on the blank-area target.
    panel.openCtxMenu(mouseEvt(), { dir: true, path: '', name: 'root', blank: true })
    panel.onCtxAction('rename')
    expect(panel.renamingPath.value).toBe('')
    panel.openCtxMenu(mouseEvt(), { dir: false, path: 'README.md', name: 'README.md' })
    panel.onCtxAction('rename')
    expect(panel.renamingPath.value).toBe('README.md')
    panel.cancelRename()

    // copyPath is file-only.
    panel.openCtxMenu(mouseEvt(), { dir: true, path: 'src', name: 'src' })
    panel.onCtxAction('copyPath')
    expect(writeText).not.toHaveBeenCalled()
    panel.openCtxMenu(mouseEvt(), { dir: false, path: 'README.md', name: 'README.md' })
    panel.onCtxAction('copyPath')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('README.md')
    expect(emit).toHaveBeenCalledWith('toast', expect.stringContaining('README.md'))

    // Clipboard rejection still reports to the user.
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: vi.fn(() => Promise.reject(new Error('denied'))) },
      configurable: true,
    })
    panel.openCtxMenu(mouseEvt(), { dir: false, path: 'README.md', name: 'README.md' })
    panel.onCtxAction('copyPath')
    await flushPromises()

    // Protected roots cannot be deleted.
    panel.openCtxMenu(mouseEvt(), { dir: true, path: 'rules', name: 'rules' })
    panel.onCtxAction('delete')
    expect(panel.confirmCfg.value).toBeNull()
    panel.openCtxMenu(mouseEvt(), { dir: true, path: 'skills', name: 'skills' })
    panel.onCtxAction('delete')
    expect(panel.confirmCfg.value).toBeNull()
    panel.openCtxMenu(mouseEvt(), { dir: false, path: 'README.md', name: 'README.md' })
    panel.onCtxAction('delete')
    expect(panel.confirmCfg.value).not.toBeNull()
    await panel.confirmOk()
    expect(props.draft.files.some((f: DraftFile) => f.path === 'README.md')).toBe(false)
    await panel.confirmOk()

    panel.openCtxMenu(mouseEvt(), { dir: false, path: 'src/main.ts', name: 'main.ts' })
    panel.onCtxAction('unknownAction')

    app.unmount()
  })

  it('starts a rename from the F2 shortcut only for files', async () => {
    const { panel, app } = withFilesPanel()
    const input = document.createElement('input')
    input.setAttribute('data-rename', '')
    input.focus = vi.fn()
    input.select = vi.fn()
    document.body.appendChild(input)

    panel.onExplorerKeydown(new KeyboardEvent('keydown', { key: 'F2' }))
    expect(panel.renamingPath.value).toBe('')

    panel.selectedTreeRow.value = row('src', true)
    panel.onExplorerKeydown(new KeyboardEvent('keydown', { key: 'F2' }))
    expect(panel.renamingPath.value).toBe('')

    panel.selectedTreeRow.value = row('src/main.ts')
    panel.onExplorerKeydown(new KeyboardEvent('keydown', { key: 'F2' }))
    expect(panel.renamingPath.value).toBe('src/main.ts')
    await nextTick()
    expect(input.focus).toHaveBeenCalled()

    panel.ctxMenu.value.open = true
    panel.onExplorerKeydown(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(panel.ctxMenu.value.open).toBe(false)

    app.unmount()
  })

  it('renames files and folders, rejecting conflicts', async () => {
    const { panel, app, emit, props } = withFilesPanel()

    // A stale row (different path) is ignored.
    panel.startRename(row('README.md'))
    panel.renameInput.value = 'X.md'
    panel.commitRename(row('src/main.ts'))
    expect(props.draft.files.some((f: DraftFile) => f.path === 'README.md')).toBe(true)

    // Empty and unchanged names are no-ops.
    panel.startRename(row('README.md'))
    panel.renameInput.value = '   '
    panel.commitRename(row('README.md'))
    expect(panel.renamingPath.value).toBe('')
    panel.startRename(row('README.md'))
    panel.renameInput.value = 'README.md'
    panel.commitRename(row('README.md'))
    expect(props.draft.files.some((f: DraftFile) => f.path === 'README.md')).toBe(true)

    // Slashes are stripped from the leaf, so the parent is preserved.
    panel.startRename(row('src/main.ts'))
    panel.renameInput.value = 'entry/point.ts'
    panel.commitRename(row('src/main.ts'))
    expect(props.draft.files.some((f: DraftFile) => f.path === 'src/entrypoint.ts')).toBe(true)

    // Conflicting target reports instead of clobbering.
    panel.startRename(row('src/entrypoint.ts'))
    panel.renameInput.value = 'base.md'
    props.draft.files.push({ path: 'src/base.md', content: '' })
    panel.commitRename(row('src/entrypoint.ts'))
    expect(emit).toHaveBeenCalledWith('error', expect.stringContaining('src/base.md'))

    // Renaming a folder rewrites children, expanded dirs and empty dirs.
    expect(panel.expanded.value.has('src')).toBe(true)
    panel.toggleDir('src')
    expect(panel.expanded.value.has('src')).toBe(false)
    panel.toggleDir('src')
    expect(panel.expanded.value.has('src')).toBe(true)
    panel.emptyDirs.value.add('src/empty')
    panel.startRename(row('src', true))
    panel.renameInput.value = 'app'
    panel.commitRename(row('src', true))
    expect(props.draft.files.some((f: DraftFile) => f.path === 'app/entrypoint.ts')).toBe(true)
    expect(panel.expanded.value.has('app')).toBe(true)
    expect(panel.emptyDirs.value.has('app/empty')).toBe(true)

    app.unmount()
  })

  it('creates files and folders inside the selected directory', async () => {
    const { panel, app, emit, props } = withFilesPanel()
    const input = document.createElement('input')
    input.setAttribute('data-create', '')
    input.focus = vi.fn()
    document.body.appendChild(input)

    // No pending create → no-op.
    panel.commitCreate()

    panel.newFile('src')
    await nextTick()
    expect(input.focus).toHaveBeenCalled()
    panel.createInput.value = '/util.ts/'
    panel.commitCreate()
    expect(props.draft.files.some((f: DraftFile) => f.path === 'src/util.ts')).toBe(true)
    expect(panel.activeFile.value?.path).toBe('src/util.ts')

    // Duplicate path is refused and the editor stays open.
    panel.newFile('src')
    panel.createInput.value = 'util.ts'
    panel.commitCreate()
    expect(emit).toHaveBeenCalledWith('error', expect.stringContaining('src/util.ts'))
    expect(panel.creating.value).not.toBeNull()
    panel.cancelCreate()

    // Blank names close the editor without touching the draft.
    panel.newFile('src')
    panel.createInput.value = '  '
    panel.commitCreate()
    expect(panel.creating.value).toBeNull()
    panel.newFile('')
    panel.createInput.value = '///'
    panel.commitCreate()
    expect(panel.creating.value).toBeNull()

    panel.newFolder('src')
    panel.createInput.value = 'nested'
    panel.commitCreate()
    expect(panel.emptyDirs.value.has('src/nested')).toBe(true)
    expect(panel.expanded.value.has('src/nested')).toBe(true)
    expect(panel.rows.value.some((r) => r.path === 'src/nested')).toBe(true)

    app.unmount()
  })

  it('deletes folders together with their subtree', async () => {
    const { panel, app, props } = withFilesPanel()
    panel.openPath('src/main.ts')
    panel.emptyDirs.value.add('src/empty')
    panel.emptyDirs.value.add('other')

    panel.deleteEntry(row('rules', true))
    expect(panel.confirmCfg.value).toBeNull()

    panel.deleteEntry(row('src', true))
    expect(panel.confirmCfg.value?.danger).toBe(true)
    await panel.confirmOk()
    expect(props.draft.files.some((f: DraftFile) => f.path.startsWith('src/'))).toBe(false)
    expect(panel.emptyDirs.value.has('src/empty')).toBe(false)
    expect(panel.emptyDirs.value.has('other')).toBe(true)
    // Deleting the open file falls back to a default selection.
    expect(panel.activeFile.value?.path).toBeTruthy()

    app.unmount()
  })

  it('imports a picked folder, skipping oversized and unreadable entries', async () => {
    const { panel, app, emit, props } = withFilesPanel()

    const pickEvent = (files: File[]) => {
      const input = document.createElement('input')
      Object.defineProperty(input, 'files', { value: files, configurable: true })
      return { target: input } as unknown as Event
    }

    // Empty pick → nothing to do.
    await panel.onFolderPick(pickEvent([]))

    const big = new File(['x'], 'big.txt')
    Object.defineProperty(big, 'size', { value: 600 * 1024 })
    await panel.onFolderPick(pickEvent([big]))
    expect(emit).toHaveBeenCalledWith('error', expect.any(String))

    const withRelPath = (name: string, rel: string, content = 'body') => {
      const f = new File([content], name, { type: 'text/plain' })
      Object.defineProperty(f, 'webkitRelativePath', { value: rel })
      return f
    }

    panel.uploadTargetDir.value = 'docs'
    await panel.onFolderPick(
      pickEvent([
        withRelPath('a.md', 'pack/a.md'),
        withRelPath('b.md', 'pack/nested/b.md'),
        // A single-segment relative path keeps its own name.
        withRelPath('c.md', 'c.md'),
        // Only separators → skipped.
        withRelPath('d.md', '//'),
      ]),
    )
    const paths = props.draft.files.map((f: DraftFile) => f.path)
    expect(paths).toContain('docs/a.md')
    expect(paths).toContain('docs/nested/b.md')
    expect(paths).toContain('docs/c.md')
    expect(panel.activeFile.value?.path).toBe('docs/a.md')
    expect(emit).toHaveBeenCalledWith('toast', expect.stringContaining('docs/'))

    // Re-import overwrites the existing content in place.
    await panel.onFolderPick(pickEvent([withRelPath('a.md', 'pack/docs/a.md', 'updated')]))
    expect(props.draft.files.find((f: DraftFile) => f.path === 'docs/a.md')?.content).toBe('updated')

    // A file whose text() rejects is skipped rather than failing the import.
    const broken = withRelPath('e.md', 'pack/e.md')
    broken.text = () => Promise.reject(new Error('unreadable'))
    await panel.onFolderPick(pickEvent([broken]))

    app.unmount()
  })

  it('positions and drives the mobile explorer overflow menu', async () => {
    const { panel, app } = withFilesPanel({ isMobile: true })
    const anchorAt = (top: number, right: number) => {
      const el = document.createElement('button')
      el.getBoundingClientRect = () =>
        ({ top, bottom: top + 24, left: right - 24, right, width: 24, height: 24, x: 0, y: 0 }) as DOMRect
      return el
    }

    // Nothing open → placement is a no-op.
    panel.placeExplorerMore()
    expect(panel.explorerMoreStyle.value).toEqual({})

    panel.toggleExplorerMore(mouseEvt(0, 0, anchorAt(10, 100)), row('src', true))
    expect(panel.explorerMore.value).toEqual({ dir: true, path: 'src', name: 'src' })
    await nextTick()
    expect(panel.explorerMoreStyle.value.top).toBe('38px')
    expect(panel.explorerMoreItemCount({ dir: true, path: 'src', name: 'src' })).toBe(4)
    expect(panel.explorerMoreItemCount({ dir: true, path: 'rules', name: 'rules' })).toBe(3)
    expect(panel.explorerMoreItemCount({ dir: false, path: 'a.md', name: 'a.md' })).toBe(2)

    // Bottom overflow flips the menu above the anchor.
    panel.explorerMoreAnchor.value = anchorAt(660, 100)
    panel.onChromeReposition()
    expect(Number.parseInt(panel.explorerMoreStyle.value.top!, 10)).toBeLessThan(660)

    // Toggling the same row closes it.
    panel.toggleExplorerMore(mouseEvt(0, 0, anchorAt(10, 100)), row('src', true))
    expect(panel.explorerMore.value).toBeNull()
    panel.onChromeReposition()

    panel.toggleExplorerMore(mouseEvt(0, 0, anchorAt(10, 100)), row('src', true))
    panel.onChromeKeydown(new KeyboardEvent('keydown', { key: 'a' }))
    expect(panel.explorerMore.value).not.toBeNull()
    panel.onChromeKeydown(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(panel.explorerMore.value).toBeNull()
    panel.onChromeKeydown(new KeyboardEvent('keydown', { key: 'Escape' }))

    // Actions.
    panel.onExplorerMoreAction('newFile')
    panel.toggleExplorerMore(mouseEvt(0, 0, anchorAt(10, 100)), row('src', true))
    panel.onExplorerMoreAction('newFile')
    expect(panel.creating.value).toEqual({ dir: 'src', kind: 'file' })
    panel.cancelCreate()

    panel.toggleExplorerMore(mouseEvt(0, 0, anchorAt(10, 100)), row('src', true))
    panel.onExplorerMoreAction('newFolder')
    expect(panel.creating.value).toEqual({ dir: 'src', kind: 'folder' })
    panel.cancelCreate()

    // File targets ignore the folder-only actions.
    panel.toggleExplorerMore(mouseEvt(0, 0, anchorAt(10, 100)), row('README.md'))
    panel.onExplorerMoreAction('newFile')
    expect(panel.creating.value).toBeNull()
    panel.toggleExplorerMore(mouseEvt(0, 0, anchorAt(10, 100)), row('README.md'))
    panel.onExplorerMoreAction('newFolder')
    expect(panel.creating.value).toBeNull()

    panel.toggleExplorerMore(mouseEvt(0, 0, anchorAt(10, 100)), row('README.md'))
    panel.onExplorerMoreAction('rename')
    expect(panel.renamingPath.value).toBe('README.md')
    panel.cancelRename()

    panel.toggleExplorerMore(mouseEvt(0, 0, anchorAt(10, 100)), row('rules', true))
    panel.onExplorerMoreAction('delete')
    expect(panel.confirmCfg.value).toBeNull()
    panel.toggleExplorerMore(mouseEvt(0, 0, anchorAt(10, 100)), row('README.md'))
    panel.onExplorerMoreAction('delete')
    expect(panel.confirmCfg.value).not.toBeNull()
    panel.confirmCfg.value = null

    panel.onDocumentClick()
    app.unmount()
  })

  it('confirms leaving a dirty editor before returning to the list', async () => {
    const { panel, app, emit, props, save } = withFilesPanel({ isMobile: true, dirty: true })

    panel.tryBackToList()
    expect(panel.leaveConfirmCfg.value).not.toBeNull()
    await panel.leaveConfirmSave()
    expect(save).toHaveBeenCalled()
    expect(emit).toHaveBeenCalledWith('update:just-saved', true)
    expect(panel.filesStep.value).toBe('list')
    expect(panel.leaveConfirmCfg.value).toBeNull()

    // A rejected save keeps the prompt on screen.
    save.mockResolvedValueOnce(false)
    panel.tryBackToList()
    await panel.leaveConfirmSave()
    expect(panel.leaveConfirmCfg.value).not.toBeNull()

    // A throwing save is surfaced through the error emit.
    panel.leaveConfirmCfg.value = {
      title: 't',
      message: 'm',
      saveText: 's',
      discardText: 'd',
      onSave: () => { throw new Error('save exploded') },
      onDiscard: () => {},
    }
    await panel.leaveConfirmSave()
    expect(emit).toHaveBeenCalledWith('error', 'save exploded')

    panel.tryBackToList()
    panel.leaveConfirmDiscard()
    expect(emit).toHaveBeenCalledWith('discard')
    expect(panel.leaveConfirmCfg.value).toBeNull()
    panel.leaveConfirmDiscard()
    await panel.leaveConfirmSave()

    // Clean editor returns immediately.
    props.dirty = false
    panel.filesStep.value = 'edit'
    panel.tryBackToList()
    expect(panel.filesStep.value).toBe('list')
    expect(panel.leaveConfirmCfg.value).toBeNull()

    app.unmount()
  })

  it('keeps the open-tab strip consistent when tabs close', async () => {
    const { panel, app, props } = withFilesPanel()
    const [readme, main, rule] = props.draft.files as DraftFile[]
    panel.openFile(readme!)
    panel.openFile(main!)
    panel.openFile(rule!)
    expect(panel.openTabs.value).toHaveLength(3)
    expect(panel.currentFile.value?.path).toBe('rules/base.md')
    expect(panel.activePath.value).toBe('rules/base.md')
    expect(panel.breadcrumb.value).toEqual(['rules', 'base.md'])

    // Re-opening an already open file does not duplicate the tab.
    panel.openFile(main!)
    expect(panel.openTabs.value).toHaveLength(3)

    const snap = panel.snapshot()
    expect(snap).toEqual({
      path: 'src/main.ts',
      openPaths: ['README.md', 'src/main.ts', 'rules/base.md'],
    })

    panel.closeTab(main!)
    expect(panel.openTabs.value).toHaveLength(2)
    expect(panel.activeFile.value?.path).toBe('rules/base.md')
    // Closing a tab that is not open is ignored.
    panel.closeTab(main!)
    expect(panel.openTabs.value).toHaveLength(2)

    panel.closeTab(rule!)
    expect(panel.activeFile.value?.path).toBe('README.md')
    panel.closeTab(readme!)
    expect(panel.activeFile.value).toBeNull()

    panel.restoreAfterDiscard(snap)
    expect(panel.openTabs.value.map((f) => f.path)).toEqual([
      'README.md',
      'src/main.ts',
      'rules/base.md',
    ])
    expect(panel.activeFile.value?.path).toBe('src/main.ts')
    panel.restoreAfterDiscard({ path: '', openPaths: ['ghost.md'] })
    expect(panel.openTabs.value).toEqual([])
    expect(panel.activeFile.value).toBeNull()

    app.unmount()
  })

  it('creates missing files on demand with a backend placeholder', async () => {
    const { panel, app, props } = withFilesPanel()

    panel.openPath('missing.md')
    expect(panel.activeFile.value?.path).not.toBe('missing.md')

    panel.openPathOrCreate(AGENT_SETTINGS_PATH)
    expect(panel.activeFile.value?.path).toBe(AGENT_SETTINGS_PATH)
    expect(panel.activeFile.value?.content).toBeTruthy()

    props.draft.acpBackend = 'opencode'
    panel.openPathOrCreate('opencode.json')
    expect(panel.activeFile.value?.path).toBe('opencode.json')
    expect(panel.activeFile.value?.content).toContain('opencode.ai/config.json')

    panel.openPathOrCreate('notes/todo.md', 'seed')
    expect(props.draft.files.find((f: DraftFile) => f.path === 'notes/todo.md')?.content).toBe('seed')
    expect(panel.expanded.value.has('notes')).toBe(true)

    panel.openPathOrCreate('plain.txt')
    expect(props.draft.files.find((f: DraftFile) => f.path === 'plain.txt')?.content).toBe('')

    // Existing paths are reused rather than re-created.
    const before = props.draft.files.length
    panel.openPathOrCreate('plain.txt')
    expect(props.draft.files).toHaveLength(before)

    app.unmount()
  })

  it('picks a sensible default file per layout', async () => {
    const { panel, app, props } = withFilesPanel()
    panel.selectDefaultFile()
    expect(panel.activeFile.value?.path).toBe('README.md')

    props.draft.files = [{ path: 'a.txt', content: '' }]
    panel.selectDefaultFile()
    expect(panel.activeFile.value?.path).toBe('a.txt')

    props.draft.files = []
    panel.selectDefaultFile()
    expect(panel.activeFile.value).toBeNull()

    props.draft.files = baseDraft().files
    panel.resetForSelect()
    expect(panel.filesStep.value).toBe('list')
    expect(panel.activeFile.value?.path).toBe('README.md')
    expect(panel.expanded.value.size).toBe(0)

    props.isMobile = true
    await nextTick()
    panel.selectDefaultFile()
    expect(panel.activeFile.value).toBeNull()
    expect(panel.filesStep.value).toBe('list')

    app.unmount()
  })

  it('follows the mobile breakpoint for step and overflow state', async () => {
    const { panel, app, props } = withFilesPanel()
    panel.openPath('README.md')
    props.isMobile = true
    await nextTick()
    expect(panel.filesStep.value).toBe('edit')

    panel.activeFile.value = null
    props.isMobile = false
    await nextTick()
    props.isMobile = true
    await nextTick()
    expect(panel.filesStep.value).toBe('list')

    panel.explorerMore.value = { dir: false, path: 'README.md', name: 'README.md' }
    props.isMobile = false
    await nextTick()
    expect(panel.explorerMore.value).toBeNull()
    expect(panel.workspaceGridStyle.value.gridTemplateColumns).toContain('240px')

    props.isMobile = true
    await nextTick()
    expect(panel.workspaceGridStyle.value).toEqual({ gridTemplateColumns: '1fr' })

    app.unmount()
  })

  it('drops the history column for the shared agent', async () => {
    const { panel, app } = withFilesPanel({ agentName: '' })
    expect(panel.workspaceGridStyle.value.gridTemplateColumns).toBe('240px 1fr')
    panel.toggleExplorerCollapsed()
    expect(panel.workspaceGridStyle.value.gridTemplateColumns).toBe('28px 1fr')
    app.unmount()
  })

  it('maps editor languages by extension', async () => {
    const { panel, app } = withFilesPanel()
    expect(panel.langForPath('a.md')).toBe('markdown')
    expect(panel.langForPath('a.markdown')).toBe('markdown')
    expect(panel.langForPath('a.json')).toBe('json')
    expect(panel.langForPath('a.sh')).toBe('shell')
    expect(panel.langForPath('a.mjs')).toBe('javascript')
    expect(panel.langForPath('a.ts')).toBe('typescript')
    expect(panel.langForPath('a.py')).toBe('python')
    expect(panel.langForPath('a.yaml')).toBe('yaml')
    expect(panel.langForPath('a.toml')).toBe('ini')
    expect(panel.langForPath('a.txt')).toBe('plaintext')
    expect(panel.langForPath('Dockerfile')).toBe('plaintext')
    expect(panel.isMdPath('A.MD')).toBe(true)
    expect(panel.isMdPath('a.ts')).toBe(false)
    expect(panel.joinPath(' src ', ' a.ts ')).toBe('src/a.ts')
    expect(panel.joinPath('', 'a.ts')).toBe('a.ts')
    app.unmount()
  })

  it('defaults history rail collapsed; explorer stays expanded when storage fails', async () => {
    const getItem = vi.spyOn(localStorage, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    const setItem = vi.spyOn(localStorage, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    const { panel, app } = withFilesPanel()
    expect(panel.explorerCollapsed.value).toBe(false)
    expect(panel.historyCollapsed.value).toBe(true)
    expect(panel.workspaceGridStyle.value.gridTemplateColumns).toBe('240px 1fr 28px')
    panel.toggleHistoryCollapsed()
    expect(panel.historyCollapsed.value).toBe(false)
    expect(panel.workspaceGridStyle.value.gridTemplateColumns).toBe('240px 1fr 300px')
    getItem.mockRestore()
    setItem.mockRestore()

    panel.writeCollapsedState(panel.EXPLORER_COLLAPSED_KEY, true)
    expect(panel.readCollapsedState(panel.EXPLORER_COLLAPSED_KEY)).toBe(true)
    app.unmount()
  })

  it('history key null/missing falls back to collapsed; false persists expanded', async () => {
    localStorage.removeItem('agent-studio-history-collapsed')
    const { panel: fresh, app: app1 } = withFilesPanel()
    expect(fresh.readCollapsedState(fresh.HISTORY_COLLAPSED_KEY, true)).toBe(true)
    expect(fresh.historyCollapsed.value).toBe(true)
    app1.unmount()

    localStorage.setItem('agent-studio-history-collapsed', 'false')
    const { panel: expanded, app: app2 } = withFilesPanel()
    expect(expanded.historyCollapsed.value).toBe(false)
    expect(expanded.workspaceGridStyle.value.gridTemplateColumns).toBe('240px 1fr 300px')
    app2.unmount()
    localStorage.removeItem('agent-studio-history-collapsed')
  })
})

describe('other history entries stay collapsed by default (plan g3.1 / g3.3)', () => {
  it('WorkflowEditorView showVersions defaults false; PreviewFeedbackChat historyExpanded defaults false', () => {
    const root = join(dirname(fileURLToPath(import.meta.url)), '../..')
    const wf = readFileSync(join(root, 'views/WorkflowEditorView.vue'), 'utf8')
    const fb = readFileSync(join(root, 'components/run/PreviewFeedbackChat.vue'), 'utf8')
    expect(wf).toMatch(/const showVersions = ref\(false\)/)
    expect(fb).toMatch(/const historyExpanded = ref\(false\)/)
  })
})
