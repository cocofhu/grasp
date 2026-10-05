/**
 * Run 详情：选中节点、nodeTab 默认策略、深链 focus、移动端主面板切换。
 * 不改变 tab 结构 / 文案 / 交互路径——仅下移编排。
 */
import { computed, ref, watch, type ComputedRef, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { useToast } from '@/lib/composables/useToast'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { isProductNode } from '@/lib/run/productNodeArtifacts'
import { isAppPreviewRemoteNode } from '@/lib/run/reactArtifactPreview'
import { resolveOutputFocusNodeId } from '@/lib/run/runOutputSelection'
import { isClarifyNode } from '@/lib/shared/clarifyInteractive'
import type { NodeRunStatus, Run, WFNode, Workflow } from '@/lib/shared/types'

export function useRunDetailSelection(opts: {
  run: Ref<Run>
  wf: Ref<Workflow>
  selected: Ref<string | null>
  manual: Ref<boolean>
  selNode: ComputedRef<WFNode | null>
  selStatus: ComputedRef<NodeRunStatus>
  selRun: ComputedRef<{ error?: string } | null>
  runLoading: Ref<boolean>
}) {
  const { t } = useI18n()
  const toast = useToast()
  const route = useRoute()
  const { isMobile } = useBreakpoint()
  const { run, wf, selected, manual, selNode, selStatus, selRun, runLoading } = opts

  // The right panel is scoped to the selected node with node-relevant tabs.
  // Gate/clarify nodes add their interaction tab; agent-like nodes (sandbox
  // execution) add the ACP execution-log tab.
  const gateActive = computed(() => !!run.value.gate && selected.value === run.value.gate.nodeId)
  // A clarify Agent IS the clarify node — surface its tab as soon as the node is
  // selected, even before the first turn has finished generating (the conversation
  // row is only created after ReactOpen returns). The panel then shows a loading
  // state until the dialogue is available.
  const clarifyActive = computed(() => isClarifyNode(selNode.value))
  // The selected clarify node's own conversation (per-node), falling back to the
  // run's current clarify when it matches this node.
  const selClarify = computed(() => {
    const id = selected.value
    if (!id) return null
    return run.value.clarifyByNode?.[id] || (run.value.clarify?.nodeId === id ? run.value.clarify : null) || null
  })
  // The clarify chat only accepts replies while the run is live AND this node is
  // genuinely waiting for human input (not a sandbox infrastructure failure).
  const clarifyInputActive = computed(
    () => ['queued', 'running', 'waiting_human'].includes(run.value.status) && selStatus.value === 'waiting_human',
  )
  // Clarify node failed during sandbox setup — show error-box instead of chat/loader.
  const clarifySandboxFailed = computed(
    () => isClarifyNode(selNode.value) && selStatus.value === 'failed' && !!selRun.value?.error,
  )

  // Agent nodes run the in-container cursor-agent, so they get both the ACP
  // 执行日志 and 沙箱日志 tabs.
  const hasLog = computed(() => selNode.value?.type === 'agent')

  // Nodes owning structured products (declared writes / proposal_select) surface
  // them in a dedicated "产物" tab.
  const hasProduct = computed(() => isProductNode(selNode.value))
  const nodeCompleted = computed(() => selStatus.value === 'completed')

  const hasAppPreview = computed(() => isAppPreviewRemoteNode(selNode.value))

  // Post-run ReAct review: a non-clarify producer node that has an open review
  // conversation (the backend only seeds one for review-capable producers). The
  // combined review tab shows the product view (annotatable) + the ReAct chat.
  const reviewActive = computed(() => {
    const n = selNode.value
    if (!n || isClarifyNode(n)) return false
    const conv = selClarify.value
    return !!conv && !conv.done
  })

  const nodeTabs = computed(() => {
    const tabs: { id: string; label: string; ghosted?: boolean; disabled?: boolean }[] = []
    if (gateActive.value) tabs.push({ id: 'gate', label: t('pages.runDetail.tabs.gate') })
    if (clarifyActive.value) tabs.push({ id: 'clarify', label: t('pages.runDetail.tabs.clarify') })
    if (reviewActive.value) tabs.push({ id: 'review', label: t('pages.runDetail.tabs.review') })
    if (hasAppPreview.value) tabs.push({ id: 'preview', label: t('pages.runDetail.tabs.appPreview') })
    if (hasProduct.value) tabs.push({ id: 'product', label: t('pages.runDetail.tabs.product') })
    tabs.push({ id: 'output', label: t('pages.runDetail.tabs.output') })
    if (hasLog.value) tabs.push({ id: 'log', label: t('pages.runDetail.tabs.log') })
    if (hasLog.value) tabs.push({ id: 'sandbox', label: t('pages.runDetail.tabs.sandbox') })
    return tabs
  })

  function onNodeTabDisabledClick(id: string) {
    if (id === 'gate') toast.warn(t('pages.runDetail.gateRemoved'))
  }
  const nodeTab = ref('output')
  /** When set, watch(selected) must not steal tab=output (QQ deep link / live complete). */
  const outputFocusLock = ref(false)

  function graphNodesForFocus() {
    return wf.value.nodes.length ? wf.value.nodes : run.value.nodes || []
  }

  function queryParam(key: string): string {
    const raw = route.query[key]
    if (typeof raw === 'string') return raw.trim()
    if (Array.isArray(raw) && typeof raw[0] === 'string') return String(raw[0]).trim()
    return ''
  }

  /**
   * Mobile (≤767) list-detail: mutually exclusive timeline vs node detail.
   * Desktop keeps side-by-side panes; this state is ignored when !isMobile.
   * Defaults: waiting_human or deep-link tab=output → detail; else timeline.
   * Live running→completed also switches to detail (see status watch).
   */
  const mobileMainPanel = ref<'timeline' | 'detail'>(
    isMobile.value && (run.value.status === 'waiting_human' || queryParam('tab') === 'output')
      ? 'detail'
      : 'timeline',
  )
  /** Bumped to re-scroll selected timeline item (e.g. back from detail). */
  const timelineScrollToken = ref(0)

  const mobileDetailPanelLabel = computed(() => {
    const tab = nodeTabs.value.find((t) => t.id === nodeTab.value)
    return tab?.label || t('pages.runDetail.tabs.output')
  })

  function showMobileTimelinePanel() {
    mobileMainPanel.value = 'timeline'
    timelineScrollToken.value += 1
  }

  function showMobileDetailPanel() {
    mobileMainPanel.value = 'detail'
  }

  function backToMobileTimeline() {
    showMobileTimelinePanel()
  }

  /** Parse ?node=&tab=output (completed QQ deep link). Returns true when applied. */
  function applyOutputDeepLinkFocus(): boolean {
    const qNode = queryParam('node')
    const qTab = queryParam('tab')
    if (qTab !== 'output' && !qNode) return false

    const nodes = graphNodesForFocus()
    const focusId =
      (qNode && nodes.some((n) => n.id === qNode) ? qNode : null) ||
      resolveOutputFocusNodeId(run.value, nodes)

    outputFocusLock.value = true
    if (focusId) {
      manual.value = false
      selected.value = focusId
    }
    nodeTab.value = 'output'
    if (isMobile.value) mobileMainPanel.value = 'detail'
    return true
  }

  // Pick a sensible default tab when the SELECTION changes (not on every poll, or
  // it would fight the user's manual tab choice): a pending human interaction
  // first, then the live log for agent-like nodes, else the overview.
  watch(
    selected,
    () => {
      if (outputFocusLock.value) {
        nodeTab.value = 'output'
        return
      }
      if (gateActive.value) nodeTab.value = 'gate'
      else if (clarifyActive.value && !run.value.clarify?.done) nodeTab.value = 'clarify'
      else if (reviewActive.value) nodeTab.value = 'review'
      else if (hasProduct.value && nodeCompleted.value) nodeTab.value = 'product'
      else if (hasLog.value) nodeTab.value = 'log'
      else nodeTab.value = 'output'
    },
    { immediate: true },
  )
  watch(nodeTab, (tab) => {
    if (outputFocusLock.value && tab !== 'output') outputFocusLock.value = false
  })
  // Live running/waiting_human→completed: select last output node, open output view,
  // mobile detail. Skip hard-load hydration (emptyRun dummy running → completed).
  watch(
    () => run.value.status,
    (st, prev) => {
      if (st !== 'completed') return
      if (runLoading.value) return
      if (prev !== 'running' && prev !== 'waiting_human') return
      const id = resolveOutputFocusNodeId(run.value, graphNodesForFocus())
      if (id) {
        manual.value = false
        selected.value = id
      }
      outputFocusLock.value = true
      nodeTab.value = 'output'
      if (isMobile.value) mobileMainPanel.value = 'detail'
    },
  )

  watch(
    () => run.value.gate?.nodeId,
    (gateNodeId) => {
      if (isMobile.value && gateNodeId && run.value.status === 'waiting_human') {
        manual.value = false
        selected.value = gateNodeId
        nodeTab.value = 'gate'
        mobileMainPanel.value = 'detail'
      }
    },
    { immediate: true },
  )

  // waiting_human without a gate (e.g. review/clarify): still prefer detail panel once.
  watch(
    () => run.value.status,
    (st, prev) => {
      if (!isMobile.value) return
      if (st === 'waiting_human' && prev !== 'waiting_human' && !run.value.gate?.nodeId) {
        mobileMainPanel.value = 'detail'
      }
    },
  )
  // If the current tab disappears (e.g. clarify resolved), fall back gracefully.
  // Ghosted/disabled tabs are never a valid active selection.
  watch(nodeTabs, (tabs) => {
    const cur = tabs.find((t) => t.id === nodeTab.value)
    if (!cur || cur.ghosted || cur.disabled) {
      nodeTab.value = tabs.find((t) => !t.ghosted && !t.disabled)?.id || 'output'
    }
  })

  function selectNode(id: string) {
    outputFocusLock.value = false
    manual.value = true
    selected.value = id
    if (isMobile.value) mobileMainPanel.value = 'detail'
  }

  return {
    isMobile,
    gateActive,
    clarifyActive,
    selClarify,
    clarifyInputActive,
    clarifySandboxFailed,
    hasLog,
    hasProduct,
    nodeCompleted,
    hasAppPreview,
    reviewActive,
    nodeTabs,
    onNodeTabDisabledClick,
    nodeTab,
    outputFocusLock,
    graphNodesForFocus,
    queryParam,
    mobileMainPanel,
    timelineScrollToken,
    mobileDetailPanelLabel,
    showMobileTimelinePanel,
    showMobileDetailPanel,
    backToMobileTimeline,
    applyOutputDeepLinkFocus,
    selectNode,
  }
}
