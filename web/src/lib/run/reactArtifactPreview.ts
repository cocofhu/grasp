import type { Artifact, Run, WFNode } from '@/lib/shared/types'
import { productArtifactName } from '@/lib/run/productNodeArtifacts'
import { isClarifyInteractive } from '@/lib/shared/clarifyInteractive'

const NODE_COMPLETE_ARTIFACT = 'node_complete.json'
const FEEDBACK_INDEX_NAME = 'feedback_index.json'
const FEEDBACK_PREFIX = 'feedback.'
const RUN_ERROR_ARTIFACT = 'run_error.json'
const PREVIEW_ANNOTATIONS_ARTIFACT = 'preview_annotations.json'

const BOOKKEEPING_EXACT_NAMES = new Set([
  NODE_COMPLETE_ARTIFACT,
  FEEDBACK_INDEX_NAME,
  RUN_ERROR_ARTIFACT,
  PREVIEW_ANNOTATIONS_ARTIFACT,
])

export const REACT_STAGE_TAB_GRID = 'grid'
/** Chrome “产物预览” empty surface (distinct from per-artifact `preview:` tabs). */
export const REACT_STAGE_TAB_PREVIEW = 'preview'
export const REACT_STAGE_TAB_NOVNC = 'novnc'
export const PREVIEW_TAB_PREFIX = 'preview:'

export type ReactStageRemoteKind = 'sandbox' | 'app' | 'public' | 'off'

/** Own-node artifacts can be annotated; foreign / unnamed-own stay view-only. */
export function isOwnNodeArtifact(artifact: Pick<Artifact, 'nodeId'> | null | undefined, nodeId?: string | null): boolean {
  const own = String(nodeId || '').trim()
  if (!own) return true
  const artNode = String(artifact?.nodeId || '').trim()
  if (!artNode) return true
  return artNode === own
}

export function canAnnotateStageArtifact(
  annotatable: boolean,
  artifact: (Pick<Artifact, 'nodeId'> & Partial<Pick<Artifact, 'id'>>) | null | undefined,
  nodeId?: string | null,
): boolean {
  return !!annotatable && isOwnNodeArtifact(artifact, nodeId) && !isHistoricalStageArtifact(artifact)
}

const HISTORICAL_PAGE_ID = /^historical-page:([^:]+):(\d+)$/

export function historicalStageArtifactId(nodeId: string, iteration: number): string {
  return `historical-page:${nodeId}:${iteration}`
}

export function parseHistoricalStageArtifact(
  artifact: Partial<Pick<Artifact, 'id'>> | null | undefined,
): { nodeId: string; iteration: number } | null {
  const m = HISTORICAL_PAGE_ID.exec(String(artifact?.id || ''))
  if (!m) return null
  return { nodeId: m[1], iteration: Number(m[2]) }
}

export function isHistoricalStageArtifact(artifact: Partial<Pick<Artifact, 'id'>> | null | undefined): boolean {
  return parseHistoricalStageArtifact(artifact) != null
}

export function historicalStageArtifactName(baseName: string, iteration: number): string {
  return `${baseName}#iter-${iteration}`
}

export function visualProductPageName(): string {
  return productArtifactName('visual') || 'page.html'
}

/** Physical copy written by finalizeVisual: `{nodeId}.page.html`. */
export function visualNodePageName(nodeId: string): string {
  return `${String(nodeId || '').trim()}.page.html`
}

export function parseVisualNodePageName(name: string | null | undefined): string | null {
  const n = String(name || '')
  const suffix = '.page.html'
  if (!n.endsWith(suffix)) return null
  const nodeId = n.slice(0, -suffix.length)
  if (!nodeId || n === visualProductPageName()) return null
  return nodeId
}

/** Hide `{nodeId}.page.html` when page.html from the same visual node is also in the grid. */
export function isSamePreviewVisualCopy(
  artifact: Pick<Artifact, 'name' | 'nodeId'>,
  artifacts: Pick<Artifact, 'name' | 'nodeId'>[],
): boolean {
  const owner = parseVisualNodePageName(artifact.name)
  if (!owner) return false
  const page = artifacts.find((a) => a.name === visualProductPageName())
  if (!page) return false
  return String(page.nodeId || '').trim() === owner
}

export type ArtifactVersionMeta = {
  artifactId: string
  revision: number
  nodeId: string
  sizeBytes: number
  createdAt: string
}

export type ArtifactVersionChoice = {
  index: number
  revision: number
  latest: boolean
  available: boolean
}

/** Live row + archived snapshots → user-visible v1..vN (live is always last). */
export function buildArtifactVersionChoices(
  liveRevision: number,
  archived: ArtifactVersionMeta[] | null | undefined,
): ArtifactVersionChoice[] {
  const live = artifactRevision({ revision: liveRevision })
  const seen = new Set<number>()
  const choices: ArtifactVersionChoice[] = []
  for (const row of archived || []) {
    const rev = artifactRevision(row)
    if (rev === live || seen.has(rev)) continue
    seen.add(rev)
    choices.push({ index: rev, revision: rev, latest: false, available: true })
  }
  choices.sort((a, b) => a.revision - b.revision)
  choices.push({ index: live, revision: live, latest: true, available: true })
  return choices
}

/**
 * Grid display list: never flatten page.html#iter-N into extra cards.
 * Hide same-preview visual_{nodeId}.page.html when page.html is present.
 */
export function expandStageArtifacts(
  artifacts: Artifact[],
  _run?: Run | null,
  _node?: WFNode | null,
): Artifact[] {
  const list = artifacts.filter((a) => !isHistoricalStageArtifact(a))
  const page = list.find((a) => a.name === visualProductPageName())
  if (!page) return list
  const owner = String(page.nodeId || '').trim()
  if (!owner) return list
  const alias = visualNodePageName(owner)
  return list.filter((a) => a.name !== alias)
}

function gridArtifactBaseName(name: string): string {
  return String(name || '').replace(/#iter-\d+$/, '')
}

function isFeedbackStageArtifactName(name: string): boolean {
  return name === FEEDBACK_INDEX_NAME || name.startsWith(FEEDBACK_PREFIX)
}

function isHumanGateBodySnapshot(
  name: string,
  run?: Run | null,
): boolean {
  if (!name.endsWith('.md') || !run?.nodes?.length) return false
  const nodeId = name.slice(0, -'.md'.length)
  if (!nodeId) return false
  return run.nodes.some((n) => n.id === nodeId && n.type === 'human_gate')
}

/** Platform bookkeeping — hidden from the pipeline grid. Everything else stays. */
export function isBookkeepingArtifact(
  artifact: Pick<Artifact, 'name' | 'nodeId'> | null | undefined,
  run?: Run | null,
): boolean {
  if (!artifact) return false
  const name = String(artifact.name || '').trim()
  if (!name) return false
  const base = gridArtifactBaseName(name)
  if (BOOKKEEPING_EXACT_NAMES.has(base) || isFeedbackStageArtifactName(base)) return true
  if (parseVisualNodePageName(base)) return true
  if (isHumanGateBodySnapshot(base, run)) return true
  return false
}

/** Pipeline-grid subset; preview tabs / pins still use the unfiltered stage list. */
export function filterStageGridArtifacts(artifacts: Artifact[], run?: Run | null): Artifact[] {
  return artifacts.filter((a) => !isBookkeepingArtifact(a, run))
}

/**
 * Grid cards: known products, plus the effective pin so a closed auto-pin
 * (e.g. react demo HTML) remains reopenable from the pipeline grid.
 */
export function stageGridArtifactsWithPin(
  artifacts: Artifact[],
  run?: Run | null,
  pin?: string | null,
): Artifact[] {
  const filtered = filterStageGridArtifacts(artifacts, run)
  const name = String(pin || '').trim()
  if (!name || filtered.some((a) => a.name === name)) return filtered
  const pinned = artifacts.find((a) => a.name === name)
  if (!pinned) return filtered
  const keepIds = new Set(filtered.map((a) => a.id))
  keepIds.add(pinned.id)
  return artifacts.filter((a) => keepIds.has(a.id))
}

export function resolveStageRemoteKind(opts: {
  remoteKind?: ReactStageRemoteKind | null
  runId?: string | null
  nodeId?: string | null
  inlineContent?: boolean
}): ReactStageRemoteKind {
  if (opts.remoteKind) return opts.remoteKind
  if (opts.inlineContent) return 'off'
  if (opts.runId && opts.nodeId) return 'sandbox'
  return 'off'
}

export type ReactStageTab =
  | typeof REACT_STAGE_TAB_GRID
  | typeof REACT_STAGE_TAB_PREVIEW
  | typeof REACT_STAGE_TAB_NOVNC
  | string

export function previewTabId(name: string): string {
  return PREVIEW_TAB_PREFIX + name
}

export function previewTabName(tabId: string): string | null {
  if (!tabId.startsWith(PREVIEW_TAB_PREFIX)) return null
  const n = tabId.slice(PREVIEW_TAB_PREFIX.length)
  return n || null
}

/** Append a preview tab; clicking an already-open artifact only focuses it. */
export function openStagePreviewTab(openNames: string[], name: string): string[] {
  const n = String(name || '').trim()
  if (!n) return openNames
  if (openNames.includes(n)) return openNames
  return [...openNames, n]
}

export function closeStagePreviewTab(openNames: string[], name: string): string[] {
  return openNames.filter((n) => n !== name)
}

/** After closing a tab, stay on the current one, else neighbor, else noVNC/preview empty. */
export function nextTabAfterClose(
  openNames: string[],
  closed: string,
  currentTab: string,
  novncOpen = false,
): string {
  const remaining = closeStagePreviewTab(openNames, closed)
  const currentName = previewTabName(currentTab)
  if (currentName !== closed) {
    return currentTab
  }
  if (!remaining.length) {
    return novncOpen ? REACT_STAGE_TAB_NOVNC : REACT_STAGE_TAB_PREVIEW
  }
  const i = openNames.indexOf(closed)
  const pick = remaining[Math.min(Math.max(i, 0), remaining.length - 1)] ?? remaining[remaining.length - 1]
  return previewTabId(pick)
}

export type ArtifactKindKey = Artifact['kind']

const KIND_I18N: Record<string, string> = {
  html: 'pages.reactArtifactStage.kindHtml',
  json: 'pages.reactArtifactStage.kindJson',
  markdown: 'pages.reactArtifactStage.kindMarkdown',
  yaml: 'pages.reactArtifactStage.kindYaml',
  image: 'pages.reactArtifactStage.kindImage',
  text: 'pages.reactArtifactStage.kindText',
}

export function artifactKindLabelKey(kind: string | undefined): string {
  return KIND_I18N[kind || ''] || 'pages.reactArtifactStage.kindFile'
}

export function artifactRevision(a: Pick<Artifact, 'revision'> | null | undefined): number {
  const n = a?.revision
  if (typeof n === 'number' && Number.isFinite(n) && n >= 1) return Math.floor(n)
  return 1
}

export function findArtifactByName(artifacts: Artifact[], name: string | null | undefined): Artifact | null {
  const n = String(name || '').trim()
  if (!n) return null
  return artifacts.find((a) => a.name === n) || null
}

const VISUAL_LIVE_PAGE = productArtifactName('visual') || 'page.html'

/** HTML kind, or .html/.htm name (runtime.artifactKind). History cards excluded by name. */
export function isHtmlStageArtifact(artifact: Pick<Artifact, 'kind' | 'name'> | null | undefined): boolean {
  if (!artifact) return false
  if (artifact.kind === 'html') return true
  const n = String(artifact.name || '').toLowerCase()
  return n.endsWith('.html') || n.endsWith('.htm')
}

export function isIterSnapshotName(name: string | null | undefined): boolean {
  return String(name || '').includes('#iter-')
}

function artifactTimeMs(a: Pick<Artifact, 'updatedAt' | 'createdAt'>): number {
  const t = Date.parse(String(a.updatedAt || a.createdAt || ''))
  return Number.isFinite(t) ? t : 0
}

/** Clarify fallback: newest own-node live HTML. Empty nodeId → no guess (avoid upstream page.html). */
export function latestOwnNodeHtmlName(artifacts: Artifact[], nodeId?: string | null): string {
  const own = String(nodeId || '').trim()
  if (!own) return ''
  const candidates = artifacts.filter((a) => {
    if (!isOwnNodeArtifact(a, own)) return false
    if (isHistoricalStageArtifact(a)) return false
    if (isIterSnapshotName(a.name)) return false
    return isHtmlStageArtifact(a)
  })
  if (!candidates.length) return ''
  candidates.sort((a, b) => {
    const dt = artifactTimeMs(b) - artifactTimeMs(a)
    if (dt !== 0) return dt
    return artifactRevision(b) - artifactRevision(a)
  })
  return String(candidates[0]?.name || '').trim()
}

/**
 * Effective default pin for Inbox / RunClarify / RunReview:
 * 1. previewArtifact if the name is already on the stage
 * 2. visual → live page.html only (never visual_*.page.html or #iter- snapshots)
 * 3. react → newest own-node HTML
 * 4. else empty (stay on pipeline grid)
 */
export function resolveEffectivePreviewPin(opts: {
  previewArtifact?: string | null
  artifacts: Artifact[]
  nodeType?: string | null
  nodeId?: string | null
}): string {
  const artifacts = opts.artifacts || []
  const names = artifacts.map((a) => a.name)
  const pin = String(opts.previewArtifact || '').trim()
  if (pin) {
    return names.includes(pin) ? pin : ''
  }
  const nodeType = String(opts.nodeType || '').trim()
  if (nodeType === 'visual') {
    const live = artifacts.find(
      (a) => a.name === VISUAL_LIVE_PAGE && isOwnNodeArtifact(a, opts.nodeId) && !isHistoricalStageArtifact(a),
    )
    return live ? VISUAL_LIVE_PAGE : ''
  }
  if (isClarifyInteractive(nodeType)) {
    return latestOwnNodeHtmlName(artifacts, opts.nodeId)
  }
  return ''
}

/** Dedicated app_preview nodes always expose the remote app tab. Approve does not. */
export function isAppPreviewRemoteNode(type: string | null | undefined): boolean {
  return type === 'app_preview'
}

/** Approve only upgrades to app after set_preview registers a port/URL. */
export function approveStageRemoteKind(hasRegisteredPreview: boolean): ReactStageRemoteKind {
  return hasRegisteredPreview ? 'app' : 'off'
}

export function isClarifyInteractiveGraphNode(run: Run | null | undefined, nodeId: string | null | undefined): boolean {
  if (!run?.nodes?.length || !nodeId) return false
  return run.nodes.some((n: WFNode) => n.id === nodeId && isClarifyInteractive(n.type))
}

export function inboxStageRemoteKind(opts: {
  appPreview: boolean
  run?: Run | null
  nodeId?: string | null
  /** When the active node is approve: true once set_preview has registered ports/URLs. */
  hasRegisteredPreview?: boolean
}): ReactStageRemoteKind {
  if (opts.appPreview) return 'app'
  const n = opts.run?.nodes?.find((node: WFNode) => node.id === opts.nodeId)
  if (isAppPreviewRemoteNode(n?.type)) return 'app'
  // Approve is clarify-interactive but must not default to sandbox/app without a registration.
  if (n?.type === 'approve') return approveStageRemoteKind(!!opts.hasRegisteredPreview)
  if (isClarifyInteractiveGraphNode(opts.run, opts.nodeId)) return 'sandbox'
  return 'off'
}

/** Copy artifacts + previewArtifact without replacing chat turns / sessions. */
export function applyPreviewArtifactFromRun(current: Run, incoming: Run): Run {
  const next: Run = { ...current, artifacts: incoming.artifacts ?? current.artifacts }
  if (incoming.clarify) {
    next.clarify = current.clarify
      ? { ...current.clarify, previewArtifact: incoming.clarify.previewArtifact }
      : incoming.clarify
  }
  if (incoming.clarifyByNode) {
    const byNode = { ...(current.clarifyByNode || {}) }
    for (const [id, conv] of Object.entries(incoming.clarifyByNode)) {
      const cur = byNode[id]
      byNode[id] = cur ? { ...cur, previewArtifact: conv.previewArtifact } : conv
    }
    next.clarifyByNode = byNode
  }
  return next
}

export function applyPreviewArtifactName(current: Run, nodeId: string, name: string): Run {
  const n = String(name || '').trim()
  if (!n || !nodeId) return current
  const byNode = { ...(current.clarifyByNode || {}) }
  const fromClarify = current.clarify?.nodeId === nodeId ? current.clarify : undefined
  const cur = byNode[nodeId] || fromClarify
  if (cur) byNode[nodeId] = { ...cur, previewArtifact: n }
  const clarify = fromClarify ? { ...fromClarify, previewArtifact: n } : current.clarify
  return { ...current, clarifyByNode: byNode, clarify }
}

/** Open/focus the pinned tab when the pin changes or the named artifact first appears. */
export function shouldActivatePinnedPreview(
  pin: string,
  names: string[],
  prevPin?: string,
  prevNames?: string[],
  userMoved = false,
): boolean {
  if (userMoved) return false
  const n = String(pin || '').trim()
  if (!n || !names.includes(n)) return false
  if (prevPin === undefined || prevNames === undefined) return true
  return n !== prevPin || !prevNames.includes(n)
}

export function artifactFingerprint(a: Artifact | null | undefined): string {
  if (!a) return ''
  return `${a.id}:${a.updatedAt || ''}:${a.revision ?? ''}:${a.sizeBytes}:${a.content?.length ?? ''}`
}

/** react / approve stages auto-pin visible own-node artifacts on create/update. */
export function isAutoPinStageNode(type: string | null | undefined): boolean {
  return isClarifyInteractive(type)
}

/**
 * User-reviewable stage artifacts: drop process files, feedback, historical
 * mirrors, and same-preview visual_{node}.page.html aliases.
 */
export function isVisibleAutoPinArtifact(
  artifact: Pick<Artifact, 'name' | 'id' | 'nodeId'> | null | undefined,
  artifacts: Pick<Artifact, 'name' | 'nodeId'>[] = [],
  run?: Run | null,
): boolean {
  if (!artifact) return false
  const name = String(artifact.name || '').trim()
  if (!name) return false
  if (isBookkeepingArtifact(artifact, run)) return false
  if (isHistoricalStageArtifact(artifact)) return false
  if (isIterSnapshotName(name)) return false
  if (isSamePreviewVisualCopy(artifact, artifacts)) return false
  return true
}

/** Visible products shown on the react/approve pipeline grid (incl. custom names). */
export function filterVisibleStageArtifacts(artifacts: Artifact[], run?: Run | null): Artifact[] {
  return artifacts.filter((a) => isVisibleAutoPinArtifact(a, artifacts, run))
}

/**
 * Grid cards for the current stage node.
 * react/approve: every visible product (so custom HTML stays reopenable).
 * Other nodes: known contract products + effective pin.
 */
export function stageGridArtifactsForNode(
  artifacts: Artifact[],
  run?: Run | null,
  pin?: string | null,
  nodeType?: string | null,
): Artifact[] {
  if (isAutoPinStageNode(nodeType)) {
    return filterVisibleStageArtifacts(artifacts, run)
  }
  return stageGridArtifactsWithPin(artifacts, run, pin)
}

export type ArtifactFingerprintMap = Record<string, string>

export function buildArtifactFingerprintMap(
  artifacts: Pick<Artifact, 'name' | 'id' | 'updatedAt' | 'revision' | 'sizeBytes' | 'content'>[],
): ArtifactFingerprintMap {
  const out: ArtifactFingerprintMap = {}
  for (const a of artifacts) {
    const name = String(a.name || '').trim()
    if (!name) continue
    out[name] = artifactFingerprint(a as Artifact)
  }
  return out
}

export type ArtifactFingerprintDiff = {
  created: string[]
  updated: string[]
}

/** Diff name→fingerprint maps; created = first seen, updated = fingerprint changed. */
export function diffArtifactFingerprints(
  prev: ArtifactFingerprintMap | null | undefined,
  next: ArtifactFingerprintMap,
): ArtifactFingerprintDiff {
  const created: string[] = []
  const updated: string[] = []
  if (!prev) return { created, updated }
  for (const [name, fp] of Object.entries(next)) {
    if (!(name in prev)) {
      created.push(name)
      continue
    }
    if (prev[name] !== fp) updated.push(name)
  }
  return { created, updated }
}

export type StageTabUnreadKind = 'new' | 'updated'

/** Idle chrome tabs where auto-pin / pin may steal focus without interrupting reading. */
export function isIdleStageTab(activeTab: string | null | undefined): boolean {
  const tab = String(activeTab || '').trim()
  return tab === REACT_STAGE_TAB_GRID || tab === REACT_STAGE_TAB_PREVIEW
}

/**
 * Whether to switch focus after ensuring a tab is open.
 * Idle chrome (empty/grid) always allows focus.
 * Auto-pin (onlyIdle) never yanks an already-open preview/remote tab.
 * Explicit set_artifact_preview may steal until the user has moved tabs.
 */
export function shouldFocusPinnedOrAutoTab(opts: {
  userMoved: boolean
  activeTab: string
  /** When true, only empty/grid may take focus (fingerprint auto-pin). */
  onlyIdle?: boolean
}): boolean {
  if (isIdleStageTab(opts.activeTab)) return true
  if (opts.onlyIdle) return false
  return !opts.userMoved
}

export function markStageTabUnread(
  marks: Record<string, StageTabUnreadKind>,
  name: string,
  kind: StageTabUnreadKind,
): Record<string, StageTabUnreadKind> {
  const n = String(name || '').trim()
  if (!n) return marks
  const prev = marks[n]
  // Prefer sticky "new" until the tab is opened; do not downgrade to "updated".
  if (prev === 'new' && kind === 'updated') return marks
  if (prev === kind) return marks
  return { ...marks, [n]: kind }
}

export function clearStageTabUnread(
  marks: Record<string, StageTabUnreadKind>,
  name: string,
): Record<string, StageTabUnreadKind> {
  const n = String(name || '').trim()
  if (!n || !(n in marks)) return marks
  const next = { ...marks }
  delete next[n]
  return next
}

/** Card thumb: text peep for known JSON / visual HTML; HtmlPreview for other grid HTML. */
export type StageCardThumb =
  | { kind: 'text'; title: string; summary: string }
  | { kind: 'html'; html: string }

const FRIENDLY_NAME_KEYS: Record<string, string> = {
  'plan.json': 'common.gateBodyLabels.plan',
  'clarified_requirement.json': 'common.gateBodyLabels.clarifiedRequirement',
  'research.json': 'common.gateBodyLabels.research',
  'proposals.json': 'common.gateBodyLabels.proposals',
  'proposal.json': 'common.gateBodyLabels.proposal',
  'test_result.json': 'common.gateBodyLabels.testResult',
  'review.json': 'common.gateBodyLabels.review',
  'implementation_result.json': 'common.gateBodyLabels.implementationResult',
}

/** page.html and same-content visual_{node}.page.html share one friendly label. */
export function isVisualPreviewArtifactName(name: string | null | undefined): boolean {
  const base = gridArtifactBaseName(String(name || '').trim())
  if (!base) return false
  if (base === visualProductPageName()) return true
  return parseVisualNodePageName(base) != null
}

/** i18n key for reserved product display names; null → keep technical file name. */
export function artifactFriendlyNameKey(name: string | null | undefined): string | null {
  const base = gridArtifactBaseName(String(name || '').trim())
  if (!base) return null
  if (FRIENDLY_NAME_KEYS[base]) return FRIENDLY_NAME_KEYS[base]
  if (isVisualPreviewArtifactName(base)) return 'common.gateBodyLabels.pagePreview'
  return null
}

/** Technical file name shown in meta (strip historical #iter-N suffix for display). */
export function artifactTechnicalDisplayName(name: string | null | undefined): string {
  const n = String(name || '').trim()
  if (!n) return ''
  return gridArtifactBaseName(n) || n
}

function nonemptyText(v: unknown): string {
  return typeof v === 'string' ? v.trim() : ''
}

/** Root-level title/summary from known contract JSON; either field is enough. */
export function parseStructuredArtifactSummary(content: string | null | undefined): {
  title: string
  summary: string
} | null {
  const raw = String(content || '').trim()
  if (!raw) return null
  try {
    const data = JSON.parse(raw) as Record<string, unknown>
    if (!data || typeof data !== 'object' || Array.isArray(data)) return null
    const title = nonemptyText(data.title)
    const summary = nonemptyText(data.summary)
    if (!title && !summary) return null
    return { title, summary }
  } catch {
    return null
  }
}

function decodeHtmlEntities(s: string): string {
  return s
    .replace(/&nbsp;/gi, ' ')
    .replace(/&lt;/gi, '<')
    .replace(/&gt;/gi, '>')
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .replace(/&#(\d+);/g, (_, n) => String.fromCharCode(Number(n)))
    .replace(/&amp;/gi, '&')
}

function stripTags(html: string): string {
  return decodeHtmlEntities(html.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim())
}

/**
 * Light HTML peep for visual preview pages: title/h1 + meta description / banner p.
 * Returns null when neither title nor summary can be extracted.
 */
export function extractVisualHtmlSummary(html: string | null | undefined): {
  title: string
  summary: string
} | null {
  const raw = String(html || '')
  if (!raw.trim()) return null

  let title = ''
  const titleMatch = raw.match(/<title[^>]*>([\s\S]*?)<\/title>/i)
  if (titleMatch) title = stripTags(titleMatch[1])
  if (!title) {
    const h1Match = raw.match(/<h1[^>]*>([\s\S]*?)<\/h1>/i)
    if (h1Match) title = stripTags(h1Match[1])
  }

  let summary = ''
  const metaMatch = raw.match(
    /<meta[^>]+name=["']description["'][^>]*content=["']([^"']*)["'][^>]*>/i,
  ) || raw.match(
    /<meta[^>]+content=["']([^"']*)["'][^>]*name=["']description["'][^>]*>/i,
  )
  if (metaMatch) summary = decodeHtmlEntities(metaMatch[1]).trim()
  if (!summary) {
    const bannerP = raw.match(/<div[^>]*class=["'][^"']*\bbanner\b[^"']*["'][^>]*>[\s\S]*?<p[^>]*>([\s\S]*?)<\/p>/i)
    if (bannerP) summary = stripTags(bannerP[1])
  }
  if (!summary) {
    const pMatch = raw.match(/<p[^>]*>([\s\S]*?)<\/p>/i)
    if (pMatch) {
      const t = stripTags(pMatch[1])
      if (t && t !== title) summary = t
    }
  }

  if (!title && !summary) return null
  return { title, summary }
}

/** Whether this grid card should try text summary peep (vs HtmlPreview / icon). */
export function wantsTextSummaryThumb(artifact: Pick<Artifact, 'kind' | 'name'> | null | undefined): boolean {
  if (!artifact) return false
  if (artifact.kind === 'json') return true
  if (isVisualPreviewArtifactName(artifact.name)) return true
  return false
}

/** Build StageCardThumb from fetched content; null → keep document icon. */
export function buildStageCardThumb(
  artifact: Pick<Artifact, 'kind' | 'name'> | null | undefined,
  content: string | null | undefined,
): StageCardThumb | null {
  if (!artifact) return null
  const body = content ?? ''
  if (wantsTextSummaryThumb(artifact)) {
    const peep =
      artifact.kind === 'json'
        ? parseStructuredArtifactSummary(body)
        : extractVisualHtmlSummary(body)
    if (!peep) return null
    return { kind: 'text', title: peep.title, summary: peep.summary }
  }
  if (artifact.kind === 'html' || isHtmlStageArtifact(artifact)) {
    if (!String(body).trim()) return null
    return { kind: 'html', html: body }
  }
  return null
}

export type StageOpenState = {
  openNames: string[]
  activeTab: string
  novncOpen: boolean
}

const STAGE_OPEN_PREFIX = 'appr.reactStageOpen:'
const stageOpenMemory = new Map<string, StageOpenState>()

export function stageOpenStateStorageKey(runId?: string | null, nodeId?: string | null): string {
  return `${STAGE_OPEN_PREFIX}${String(runId || '').trim()}:${String(nodeId || '').trim()}`
}

export function loadStageOpenState(runId?: string | null, nodeId?: string | null): StageOpenState | null {
  const rid = String(runId || '').trim()
  const nid = String(nodeId || '').trim()
  if (!rid || !nid) return null
  const key = stageOpenStateStorageKey(rid, nid)
  const mem = stageOpenMemory.get(key)
  if (mem) {
    return {
      openNames: [...mem.openNames],
      activeTab: mem.activeTab,
      novncOpen: mem.novncOpen,
    }
  }
  if (typeof sessionStorage === 'undefined') return null
  try {
    const raw = sessionStorage.getItem(key)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<StageOpenState>
    if (!parsed || !Array.isArray(parsed.openNames)) return null
    const openNames = parsed.openNames.map((n) => String(n || '').trim()).filter(Boolean)
    const activeTab = String(parsed.activeTab || REACT_STAGE_TAB_GRID).trim() || REACT_STAGE_TAB_GRID
    const state: StageOpenState = {
      openNames,
      activeTab,
      novncOpen: !!parsed.novncOpen,
    }
    stageOpenMemory.set(key, state)
    return {
      openNames: [...state.openNames],
      activeTab: state.activeTab,
      novncOpen: state.novncOpen,
    }
  } catch {
    return null
  }
}

export function saveStageOpenState(
  runId: string | null | undefined,
  nodeId: string | null | undefined,
  state: StageOpenState,
): void {
  const rid = String(runId || '').trim()
  const nid = String(nodeId || '').trim()
  if (!rid || !nid) return
  const key = stageOpenStateStorageKey(rid, nid)
  const next: StageOpenState = {
    openNames: [...state.openNames],
    activeTab: state.activeTab,
    novncOpen: !!state.novncOpen,
  }
  stageOpenMemory.set(key, next)
  if (typeof sessionStorage === 'undefined') return
  try {
    sessionStorage.setItem(key, JSON.stringify(next))
  } catch {
    // quota / private mode — memory still holds refresh-within-SPA; hard reload needs storage
  }
}

/** Apply persisted open state against currently available artifact names. */
export function restoreStageOpenState(
  saved: StageOpenState | null | undefined,
  availableNames: string[],
): StageOpenState | null {
  if (!saved) return null
  const hasAny = saved.openNames.length > 0 || !!saved.novncOpen
  if (!hasAny && (!saved.activeTab || saved.activeTab === REACT_STAGE_TAB_GRID)) return null

  // Artifacts may not be loaded on first paint — keep names; gone-filter watch prunes later.
  if (!availableNames.length) {
    return {
      openNames: [...saved.openNames],
      activeTab: String(saved.activeTab || REACT_STAGE_TAB_GRID) || REACT_STAGE_TAB_GRID,
      novncOpen: !!saved.novncOpen,
    }
  }

  const nameSet = new Set(availableNames)
  const openNames = saved.openNames.filter((n) => nameSet.has(n))
  let activeTab = String(saved.activeTab || REACT_STAGE_TAB_GRID) || REACT_STAGE_TAB_GRID
  const activeName = previewTabName(activeTab)
  if (activeName && !openNames.includes(activeName)) {
    activeTab = openNames.length
      ? previewTabId(openNames[openNames.length - 1])
      : saved.novncOpen
        ? REACT_STAGE_TAB_NOVNC
        : REACT_STAGE_TAB_GRID
  } else if (activeTab === REACT_STAGE_TAB_NOVNC && !saved.novncOpen) {
    activeTab = openNames.length ? previewTabId(openNames[openNames.length - 1]) : REACT_STAGE_TAB_GRID
  }
  if (!openNames.length && !saved.novncOpen && activeTab === REACT_STAGE_TAB_GRID) return null
  return {
    openNames,
    activeTab,
    novncOpen: !!saved.novncOpen,
  }
}

/** Test-only: clear stage open session keys. Optional needle limits which keys are removed. */
export function resetStageOpenStateForTests(needle?: string): void {
  const match = String(needle || '').trim()
  for (const key of [...stageOpenMemory.keys()]) {
    if (!match || key.includes(match)) stageOpenMemory.delete(key)
  }
  if (typeof sessionStorage === 'undefined') return
  const keys: string[] = []
  for (let i = 0; i < sessionStorage.length; i++) {
    const k = sessionStorage.key(i)
    if (!k?.startsWith(STAGE_OPEN_PREFIX)) continue
    if (match && !k.includes(match)) continue
    keys.push(k)
  }
  for (const k of keys) sessionStorage.removeItem(k)
}
