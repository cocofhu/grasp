/**
 * Agent Studio files panel: explorer tree, tabs, read/write orchestration.
 */
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { agentConfigRelPath, defaultSettingsPlaceholder } from '@/lib/agent/backendAuthGuide'
import type { AgentStudioDraft, DraftFile } from '@/lib/agent/agentStudioDraft'
import type { CtxTarget } from '@/components/agent/ExplorerContextMenu.vue'

type FilesStep = 'list' | 'edit'
type FilesOpenSnapshot = { path: string; openPaths: string[] }

export interface AgentFilesPanelProps {
  draft: AgentStudioDraft
  dirty: boolean
  isMobile: boolean
  /** Empty/omitted for Shared Agent (out of scope for workspace VCS). */
  agentName?: string
  historyRefreshKey?: number
  save: (reason?: string) => Promise<boolean>
}

export type AgentFilesPanelEmit = {
  (e: 'error', message: string): void
  (e: 'toast', message: string): void
  (e: 'update:just-saved', value: boolean): void
  (e: 'discard'): void
  (e: 'restored'): void
}


export function useAgentFilesPanel(props: AgentFilesPanelProps, emit: AgentFilesPanelEmit) {
const { t } = useI18n()

const EXPLORER_COLLAPSED_KEY = 'agent-studio-explorer-collapsed'
const HISTORY_COLLAPSED_KEY = 'agent-studio-history-collapsed'
const SIDEBAR_EXPANDED_W = '240px'
const SIDEBAR_COLLAPSED_W = '28px'
const HISTORY_EXPANDED_W = '300px'
const HISTORY_COLLAPSED_W = '28px'

function readCollapsedState(key: string, defaultCollapsed = false): boolean {
  try {
    const v = localStorage.getItem(key)
    if (v === null) return defaultCollapsed
    return v === 'true'
  } catch {
    return defaultCollapsed
  }
}

function writeCollapsedState(key: string, collapsed: boolean) {
  try {
    localStorage.setItem(key, String(collapsed))
  } catch {
    /* ignore */
  }
}

const explorerCollapsed = ref(false)
function toggleExplorerCollapsed() {
  explorerCollapsed.value = !explorerCollapsed.value
  writeCollapsedState(EXPLORER_COLLAPSED_KEY, explorerCollapsed.value)
}

/** plan g3.2: version history defaults to collapsed (narrow rail) */
const historyCollapsed = ref(true)
function toggleHistoryCollapsed() {
  historyCollapsed.value = !historyCollapsed.value
  writeCollapsedState(HISTORY_COLLAPSED_KEY, historyCollapsed.value)
}

const workspaceGridStyle = computed(() => {
  if (props.isMobile) return { gridTemplateColumns: '1fr' }
  const explorer = explorerCollapsed.value ? SIDEBAR_COLLAPSED_W : SIDEBAR_EXPANDED_W
  if (props.agentName) {
    const history = historyCollapsed.value ? HISTORY_COLLAPSED_W : HISTORY_EXPANDED_W
    return { gridTemplateColumns: `${explorer} 1fr ${history}` }
  }
  return { gridTemplateColumns: `${explorer} 1fr` }
})

const collapseBtnClass =
  'flex shrink-0 items-center justify-center rounded w-[22px] h-[22px] text-txt3 transition hover:bg-elevated hover:text-accent-2 focus-visible:shadow-[inset_0_0_0_2px_rgba(99,102,241,0.35)] outline-none'

const filesStep = ref<FilesStep>('list')
const activeFile = ref<DraftFile | null>(null)
const openTabs = ref<DraftFile[]>([])
const expanded = ref<Set<string>>(new Set())
const emptyDirs = ref<Set<string>>(new Set())
const renamingPath = ref('')
const renameInput = ref('')
const creating = ref<{ dir: string; kind: 'file' | 'folder' } | null>(null)
const createInput = ref('')
const folderInput = ref<HTMLInputElement | null>(null)
const uploadTargetDir = ref('')
const selectedTreeRow = ref<TreeRow | null>(null)
const ctxMenu = ref<{ open: boolean; x: number; y: number; target: CtxTarget | null }>({
  open: false,
  x: 0,
  y: 0,
  target: null,
})

type ExplorerMoreTarget = { dir: boolean; path: string; name: string }
const explorerMore = ref<ExplorerMoreTarget | null>(null)
const explorerMoreStyle = ref<Record<string, string>>({})
const explorerMoreAnchor = ref<HTMLElement | null>(null)

type ConfirmCfg = {
  title: string
  message: string
  confirmText?: string
  danger?: boolean
  ok: () => void | Promise<void>
}
const confirmCfg = ref<ConfirmCfg | null>(null)

type LeaveConfirmCfg = {
  title: string
  message: string
  saveText: string
  discardText: string
  onSave: () => boolean | Promise<boolean>
  onDiscard: () => void
}
const leaveConfirmCfg = ref<LeaveConfirmCfg | null>(null)

const currentFile = computed(() => activeFile.value)
const activePath = computed(() => activeFile.value?.path || '')
const breadcrumb = computed(() => activePath.value.split('/').filter(Boolean))

function langForPath(p: string): string {
  const ext = p.split('.').pop()?.toLowerCase() || ''
  return ({ md: 'markdown', markdown: 'markdown', json: 'json', sh: 'shell', bash: 'shell', zsh: 'shell', js: 'javascript', mjs: 'javascript', ts: 'typescript', py: 'python', yml: 'yaml', yaml: 'yaml', toml: 'ini', txt: 'plaintext' } as Record<string, string>)[ext] || 'plaintext'
}

function isMdPath(p: string): boolean {
  return p.toLowerCase().endsWith('.md')
}

function hideCtxMenu() {
  ctxMenu.value.open = false
  ctxMenu.value.target = null
}

function openCtxMenu(e: MouseEvent, target: CtxTarget) {
  e.preventDefault()
  e.stopPropagation()
  selectedTreeRow.value = target.blank || target.dir
    ? null
    : { name: target.name, path: target.path, dir: false, depth: 0 }
  ctxMenu.value = { open: true, x: e.clientX, y: e.clientY, target }
  nextTick(() => {
    const menu = document.querySelector('.explorer-ctx-menu') as HTMLElement | null
    if (!menu) return
    const rect = menu.getBoundingClientRect()
    ctxMenu.value.x = Math.min(e.clientX, window.innerWidth - rect.width - 8)
    ctxMenu.value.y = Math.min(e.clientY, window.innerHeight - rect.height - 8)
  })
}

function onExplorerBlankCtx(e: MouseEvent) {
  openCtxMenu(e, { dir: true, path: '', name: t('pages.agentStudio.configPaths.root'), blank: true })
}

function onCtxAction(action: string) {
  const target = ctxMenu.value.target
  if (!target) return
  const parentDir = target.path.includes('/') ? target.path.slice(0, target.path.lastIndexOf('/')) : ''
  const row: TreeRow = { name: target.name, path: target.path, dir: target.dir, depth: 0 }

  switch (action) {
    case 'newFile':
      newFile(target.blank ? '' : target.dir ? target.path : parentDir)
      break
    case 'newFolder':
      newFolder(target.blank ? '' : target.dir ? target.path : parentDir)
      break
    case 'uploadFolder':
      if (!target.dir || target.blank) return
      uploadTargetDir.value = target.path
      folderInput.value?.click()
      break
    case 'rename':
      if (target.blank) return
      startRename(row)
      break
    case 'copyPath':
      if (target.dir) return
      navigator.clipboard?.writeText(target.path).then(
        () => emit('toast', t('common.toast.pathCopied', { path: target.path })),
        () => emit('toast', t('common.toast.copyPathFailed')),
      )
      break
    case 'delete':
      if (target.path === 'rules' || target.path === 'skills') return
      deleteEntry(row)
      break
  }
  hideCtxMenu()
}

function onExplorerKeydown(e: KeyboardEvent) {
  if (e.key === 'F2' && selectedTreeRow.value && !selectedTreeRow.value.dir) {
    e.preventDefault()
    startRename(selectedTreeRow.value)
  }
  if (e.key === 'Escape') hideCtxMenu()
}

type TreeNode = { name: string; path: string; dir: boolean; children: Record<string, TreeNode> }
type TreeRow = { name: string; path: string; dir: boolean; depth: number }

function joinPath(dir: string, name: string): string {
  return [dir, name].filter(Boolean).join('/').split('/').map((s) => s.trim()).filter(Boolean).join('/')
}

function expandParents(p: string) {
  const segs = p.split('/').filter(Boolean)
  let acc = ''
  for (let i = 0; i < segs.length - 1; i++) {
    acc = acc ? `${acc}/${segs[i]}` : segs[i]
    expanded.value.add(acc)
  }
}

const tree = computed<TreeNode>(() => {
  const root: TreeNode = { name: '', path: '', dir: true, children: {} }
  const add = (path: string, isFile: boolean) => {
    const segs = path.split('/').filter(Boolean)
    let cur = root
    let acc = ''
    segs.forEach((seg, i) => {
      acc = acc ? `${acc}/${seg}` : seg
      const leaf = isFile && i === segs.length - 1
      if (!cur.children[seg]) cur.children[seg] = { name: seg, path: acc, dir: !leaf, children: {} }
      cur = cur.children[seg]
    })
  }
  for (const f of props.draft.files || []) add(f.path, true)
  for (const d of emptyDirs.value) add(d, false)
  return root
})

const rows = computed<TreeRow[]>(() => {
  const out: TreeRow[] = []
  const walk = (node: TreeNode, depth: number) => {
    const kids = Object.values(node.children).sort((a, b) =>
      a.dir === b.dir ? a.name.localeCompare(b.name) : a.dir ? -1 : 1,
    )
    for (const k of kids) {
      out.push({ name: k.name, path: k.path, dir: k.dir, depth })
      if (k.dir && expanded.value.has(k.path)) walk(k, depth + 1)
    }
  }
  walk(tree.value, 0)
  return out
})

function openFile(f: DraftFile) {
  activeFile.value = f
  if (!openTabs.value.includes(f)) openTabs.value.push(f)
  expandParents(f.path)
  selectedTreeRow.value = { name: f.path.split('/').pop() || f.path, path: f.path, dir: false, depth: 0 }
  if (props.isMobile) filesStep.value = 'edit'
}

function openPath(path: string) {
  const f = props.draft.files.find((x) => x.path === path)
  if (f) openFile(f)
}

function openPathOrCreate(path: string, placeholder?: string) {
  let f = props.draft.files.find((x) => x.path === path)
  if (!f) {
    f = {
      path,
      content:
        placeholder ??
        (path === agentConfigRelPath(props.draft.acpBackend || 'cursor')
          ? defaultSettingsPlaceholder(props.draft.acpBackend || 'cursor')
          : ''),
    }
    props.draft.files.push(f)
    props.draft.files.sort((a, b) => a.path.localeCompare(b.path))
  }
  openFile(f)
}

function selectDefaultFile() {
  if (props.isMobile) {
    activeFile.value = null
    openTabs.value = []
    filesStep.value = 'list'
    return
  }
  const files = [...(props.draft.files || [])].sort((a, b) => a.path.localeCompare(b.path))
  const target = files.find((f) => f.path.toLowerCase().endsWith('.md')) || files[0]
  if (target) openFile(target)
  else activeFile.value = null
}

function resetForSelect() {
  filesStep.value = 'list'
  expanded.value = new Set()
  emptyDirs.value = new Set()
  openTabs.value = []
  activeFile.value = null
  renamingPath.value = ''
  creating.value = null
  closeExplorerMore()
  hideCtxMenu()
  leaveConfirmCfg.value = null
  selectDefaultFile()
}

function snapshot(): FilesOpenSnapshot {
  return {
    path: activeFile.value?.path || '',
    openPaths: openTabs.value.map((f) => f.path),
  }
}

function restoreAfterDiscard(snap: FilesOpenSnapshot) {
  openTabs.value = snap.openPaths
    .map((p) => props.draft.files.find((f) => f.path === p))
    .filter((f): f is DraftFile => !!f)
  activeFile.value = snap.path ? props.draft.files.find((f) => f.path === snap.path) || null : null
}

function goFilesList() {
  filesStep.value = 'list'
}

function tryBackToList() {
  if (!props.dirty) {
    goFilesList()
    return
  }
  leaveConfirmCfg.value = {
    title: t('pages.agentStudio.dialogs.leaveUnsavedTitle'),
    message: t('pages.agentStudio.dialogs.leaveUnsavedBackMessage'),
    saveText: t('pages.agentStudio.dialogs.saveAndBack'),
    discardText: t('pages.agentStudio.dialogs.discardChanges'),
    onSave: async () => {
      const ok = await props.save()
      if (!ok) return false
      emit('update:just-saved', true)
      goFilesList()
      return true
    },
    onDiscard: () => {
      emit('discard')
      goFilesList()
    },
  }
}

async function leaveConfirmSave() {
  const c = leaveConfirmCfg.value
  if (!c) return
  try {
    const ok = await c.onSave()
    if (!ok) return
  } catch (e: any) {
    emit('error', String(e?.message || e))
    return
  }
  leaveConfirmCfg.value = null
}

function leaveConfirmDiscard() {
  const c = leaveConfirmCfg.value
  if (!c) return
  c.onDiscard()
  leaveConfirmCfg.value = null
}

function leaveConfirmCancel() {
  leaveConfirmCfg.value = null
}

function closeTab(f: DraftFile) {
  const i = openTabs.value.indexOf(f)
  if (i < 0) return
  openTabs.value.splice(i, 1)
  if (activeFile.value === f) activeFile.value = openTabs.value[i] || openTabs.value[i - 1] || openTabs.value[openTabs.value.length - 1] || null
}

function startRename(row: TreeRow) {
  renamingPath.value = row.path
  renameInput.value = row.name
  nextTick(() => {
    const el = document.querySelector<HTMLInputElement>('input[data-rename]')
    el?.focus()
    el?.select()
  })
}

function cancelRename() {
  renamingPath.value = ''
}

function commitRename(row: TreeRow) {
  if (renamingPath.value !== row.path) return
  const leaf = renameInput.value.trim().replace(/\//g, '')
  renamingPath.value = ''
  if (!leaf || leaf === row.name) return
  const parent = row.path.includes('/') ? row.path.slice(0, row.path.lastIndexOf('/')) : ''
  const np = joinPath(parent, leaf)
  if (np === row.path) return
  if (props.draft.files.some((f) => f.path === np)) {
    emit('error', t('pages.agentStudio.dialogs.pathExists', { path: np }))
    return
  }
  if (row.dir) {
    const pref = row.path + '/'
    props.draft.files.forEach((f) => {
      if (f.path === row.path || f.path.startsWith(pref)) f.path = np + f.path.slice(row.path.length)
    })
    const ed = new Set<string>()
    expanded.value.forEach((d) => ed.add(d === row.path || d.startsWith(pref) ? np + d.slice(row.path.length) : d))
    expanded.value = ed
    const ned = new Set<string>()
    emptyDirs.value.forEach((d) => ned.add(d === row.path || d.startsWith(pref) ? np + d.slice(row.path.length) : d))
    emptyDirs.value = ned
  } else {
    const f = props.draft.files.find((x) => x.path === row.path)
    if (f) f.path = np
  }
  expandParents(np)
}

function toggleDir(path: string) {
  if (expanded.value.has(path)) expanded.value.delete(path)
  else expanded.value.add(path)
}

function newFile(dir = '') { startCreate('file', dir) }
function newFolder(dir = '') { startCreate('folder', dir) }

function startCreate(kind: 'file' | 'folder', dir = '') {
  renamingPath.value = ''
  emit('error', '')
  createInput.value = ''
  creating.value = { dir, kind }
  if (dir) {
    expandParents(dir)
    expanded.value.add(dir)
  }
  nextTick(() => {
    const el = document.querySelector<HTMLInputElement>('input[data-create]')
    el?.focus()
  })
}

function cancelCreate() {
  creating.value = null
  createInput.value = ''
}

function commitCreate() {
  const c = creating.value
  if (!c) return
  const leaf = createInput.value.trim().replace(/^\/+|\/+$/g, '')
  if (!leaf) { cancelCreate(); return }
  const full = joinPath(c.dir, leaf)
  if (!full) { cancelCreate(); return }
  if (c.kind === 'file') {
    if (props.draft.files.some((f) => f.path === full)) {
      emit('error', t('pages.agentStudio.dialogs.pathExists', { path: full }))
      return
    }
    const f = { path: full, content: '' }
    props.draft.files.push(f)
    emptyDirs.value.delete(c.dir)
    expandParents(full)
    openFile(f)
  } else {
    emptyDirs.value.add(full)
    expandParents(full)
    expanded.value.add(full)
  }
  creating.value = null
  createInput.value = ''
}

function isProtectedDir(path: string) {
  return path === 'rules' || path === 'skills'
}

function deleteEntry(row: TreeRow) {
  if (row.dir && isProtectedDir(row.path)) return
  confirmCfg.value = {
    title: row.dir ? t('pages.agentStudio.dialogs.deleteFolderTitle') : t('pages.agentStudio.dialogs.deleteFileTitle'),
    message: row.dir ? t('pages.agentStudio.dialogs.deleteFolderMessage', { path: row.path }) : t('pages.agentStudio.dialogs.deleteFileMessage', { path: row.path }),
    confirmText: t('pages.agentStudio.dialogs.delete'),
    danger: true,
    ok: () => {
      const gone = row.dir
        ? props.draft.files.filter((f) => f.path === row.path || f.path.startsWith(row.path + '/'))
        : props.draft.files.filter((f) => f.path === row.path)
      props.draft.files = props.draft.files.filter((f) => !gone.includes(f))
      openTabs.value = openTabs.value.filter((f) => !gone.includes(f))
      if (activeFile.value && gone.includes(activeFile.value)) activeFile.value = openTabs.value[openTabs.value.length - 1] || null
      if (row.dir) {
        const pref = row.path + '/'
        const ed = new Set<string>()
        emptyDirs.value.forEach((d) => { if (!(d === row.path || d.startsWith(pref))) ed.add(d) })
        emptyDirs.value = ed
      }
      if (!activeFile.value) selectDefaultFile()
    },
  }
}

async function confirmOk() {
  const c = confirmCfg.value
  if (!c) return
  confirmCfg.value = null
  await c.ok()
}

async function onFolderPick(e: Event) {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (!files.length) return
  const targetDir = uploadTargetDir.value
  uploadTargetDir.value = ''
  let added = 0
  let first = ''
  for (const f of files) {
    if (f.size > 512 * 1024) continue
    const parts = (f.webkitRelativePath || f.name).split('/').map((s) => s.trim()).filter(Boolean)
    const rel = parts.slice(1).join('/') || parts.join('/')
    if (!rel) continue
    const path = targetDir ? joinPath(targetDir, rel) : rel
    try {
      const content = await f.text()
      const at = props.draft.files.findIndex((x) => x.path === path)
      if (at >= 0) props.draft.files[at].content = content
      else props.draft.files.push({ path, content })
      if (!first) first = path
      added++
    } catch {
      /* skip */
    }
  }
  if (!added) {
    emit('error', t('pages.agentStudio.dialogs.importNoText'))
    return
  }
  props.draft.files.sort((a, b) => a.path.localeCompare(b.path))
  if (first) openPath(first)
  emit('toast', t('common.toast.importedFiles', { count: added, dir: targetDir ? targetDir + '/' : t('pages.agentStudio.configPaths.root') }))
}

function closeExplorerMore() {
  explorerMore.value = null
  explorerMoreAnchor.value = null
}

function explorerMoreItemCount(target: ExplorerMoreTarget) {
  if (target.dir) return isProtectedDir(target.path) ? 3 : 4
  return 2
}

function placeExplorerMore() {
  const anchor = explorerMoreAnchor.value
  const target = explorerMore.value
  if (!anchor || !target) return
  const rect = anchor.getBoundingClientRect()
  const margin = 8
  const menuWidth = Math.min(220, Math.max(160, window.innerWidth - margin * 2))
  const menuHeight = explorerMoreItemCount(target) * 44 + 8
  let left = rect.right - menuWidth
  left = Math.max(margin, Math.min(left, window.innerWidth - margin - menuWidth))
  let top = rect.bottom + 4
  if (top + menuHeight > window.innerHeight - margin) {
    top = Math.max(margin, rect.top - menuHeight - 4)
  }
  explorerMoreStyle.value = {
    top: `${Math.round(top)}px`,
    left: `${Math.round(left)}px`,
    width: `${Math.round(menuWidth)}px`,
  }
}

function toggleExplorerMore(e: MouseEvent, row: TreeRow) {
  const el = e.currentTarget as HTMLElement
  if (explorerMore.value?.path === row.path) {
    closeExplorerMore()
    return
  }
  hideCtxMenu()
  explorerMore.value = { dir: row.dir, path: row.path, name: row.name }
  explorerMoreAnchor.value = el
  nextTick(placeExplorerMore)
}

function onExplorerMoreAction(action: 'newFile' | 'newFolder' | 'rename' | 'delete') {
  const target = explorerMore.value
  if (!target) return
  const row: TreeRow = { name: target.name, path: target.path, dir: target.dir, depth: 0 }
  closeExplorerMore()
  if (action === 'newFile' && target.dir) newFile(target.path)
  else if (action === 'newFolder' && target.dir) newFolder(target.path)
  else if (action === 'rename') startRename(row)
  else if (action === 'delete' && !(target.dir && isProtectedDir(target.path))) deleteEntry(row)
}

function onDocumentClick() {
  hideCtxMenu()
}

function onChromeReposition() {
  if (explorerMore.value) placeExplorerMore()
}

function onChromeKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && explorerMore.value) closeExplorerMore()
}

watch(
  () => props.isMobile,
  (mobile) => {
    if (mobile) {
      filesStep.value = activeFile.value ? 'edit' : 'list'
    } else {
      closeExplorerMore()
    }
  },
)

onMounted(() => {
  explorerCollapsed.value = readCollapsedState(EXPLORER_COLLAPSED_KEY)
  // HISTORY key missing/unavailable → collapsed (true); explorer still defaults expanded
  historyCollapsed.value = readCollapsedState(HISTORY_COLLAPSED_KEY, true)
  document.addEventListener('click', onDocumentClick)
  document.addEventListener('keydown', onExplorerKeydown)
  document.addEventListener('keydown', onChromeKeydown)
  window.addEventListener('resize', onChromeReposition)
  window.addEventListener('scroll', onChromeReposition, true)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocumentClick)
  document.removeEventListener('keydown', onExplorerKeydown)
  document.removeEventListener('keydown', onChromeKeydown)
  window.removeEventListener('resize', onChromeReposition)
  window.removeEventListener('scroll', onChromeReposition, true)
})


  return {
  t,
  EXPLORER_COLLAPSED_KEY,
  HISTORY_COLLAPSED_KEY,
  SIDEBAR_EXPANDED_W,
  SIDEBAR_COLLAPSED_W,
  HISTORY_EXPANDED_W,
  HISTORY_COLLAPSED_W,
  readCollapsedState,
  writeCollapsedState,
  explorerCollapsed,
  toggleExplorerCollapsed,
  historyCollapsed,
  toggleHistoryCollapsed,
  workspaceGridStyle,
  collapseBtnClass,
  filesStep,
  activeFile,
  openTabs,
  expanded,
  emptyDirs,
  renamingPath,
  renameInput,
  creating,
  createInput,
  folderInput,
  uploadTargetDir,
  selectedTreeRow,
  ctxMenu,
  explorerMore,
  explorerMoreStyle,
  explorerMoreAnchor,
  confirmCfg,
  leaveConfirmCfg,
  currentFile,
  activePath,
  breadcrumb,
  langForPath,
  isMdPath,
  hideCtxMenu,
  openCtxMenu,
  onExplorerBlankCtx,
  onCtxAction,
  onExplorerKeydown,
  joinPath,
  expandParents,
  tree,
  rows,
  openFile,
  openPath,
  openPathOrCreate,
  selectDefaultFile,
  resetForSelect,
  snapshot,
  restoreAfterDiscard,
  goFilesList,
  tryBackToList,
  leaveConfirmSave,
  leaveConfirmDiscard,
  leaveConfirmCancel,
  closeTab,
  startRename,
  cancelRename,
  commitRename,
  toggleDir,
  newFile,
  newFolder,
  startCreate,
  cancelCreate,
  commitCreate,
  isProtectedDir,
  deleteEntry,
  confirmOk,
  onFolderPick,
  closeExplorerMore,
  explorerMoreItemCount,
  placeExplorerMore,
  toggleExplorerMore,
  onExplorerMoreAction,
  onDocumentClick,
  onChromeReposition,
  onChromeKeydown,
  }
}
