import '../src/styles/global.css'
import { computed, createApp, defineComponent, h, provide, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import InboxPendingCard from '../src/components/inbox/InboxPendingCard.vue'
import GateShareLinkPanel from '../src/components/run/GateShareLinkPanel.vue'
import ReviewComposer from '../src/components/run/ReviewComposer.vue'
import PublicGateApprovalView from '../src/views/PublicGateApprovalView.vue'
import ToastHost from '../src/components/ui/ToastHost.vue'
import { api } from '../src/lib/api/api'
import { rememberShareUrl } from '../src/lib/inbox/gateShareLink'
import type { ClarifyInboxItem, GateInboxItem, InboxItem } from '../src/lib/shared/types'
import HtmlPreview from '../src/components/ui/HtmlPreview.vue'

installIdleScrollbar()
setTheme('dark')

/** Fixture WS: public workbench must not hit a real /events socket in e2e. */
class E2ePublicWebSocket {
  url: string
  onopen: ((ev?: unknown) => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: ((ev?: unknown) => void) | null = null
  readyState = 1
  constructor(url: string) {
    this.url = url
    queueMicrotask(() => this.onopen?.())
  }
  send(data: string) {
    try {
      const m = JSON.parse(String(data || '')) as { token?: string }
      if (m.token) {
        queueMicrotask(() => this.onmessage?.({ data: JSON.stringify({ type: 'ready' }) }))
      }
    } catch {
      /* ignore */
    }
  }
  close() {
    this.readyState = 3
    this.onclose?.()
  }
}
;(window as unknown as { WebSocket: typeof WebSocket }).WebSocket =
  E2ePublicWebSocket as unknown as typeof WebSocket

const TOKEN = 'ab'.repeat(32)
/** Non-loopback mint: covers auto-copy / clipboard assertions (g2.3 / g3.1). */
const SHARE_URL_REACHABLE = `https://approving.example.com/public/gate-approvals#t=${TOKEN}`
/** Loopback mint: covers warning + disabled copy (no clipboard write). */
const SHARE_URL_LOOPBACK = `http://127.0.0.1:5174/public/gate-approvals#t=${TOKEN}`

function fixtureShareUrl(token = TOKEN): string {
  const host = new URLSearchParams(location.search).get('shareHost')
  const base = host === 'loopback' ? SHARE_URL_LOOPBACK : SHARE_URL_REACHABLE
  return base.replace(TOKEN, token)
}

let reviewPreviewBusy = false
let reviewPreviewTurns: Array<{ role: string; text: string; at: string }> = [
  { role: 'agent', text: '请复审 research.json', at: '2026-08-01T00:00:00Z' },
]
let clarifyQueue: Array<{ id: string; text: string }> = []
let clarifyConfirmUnfinished = false
let clarifyLinkUsed = false

;(window as unknown as { __idleReview?: () => void }).__idleReview = () => {
  // Authoritative idle: clear busy + pending queue so public-clarify waiting=0
  // matches product poll (confirm gated on local !sessionBusy).
  reviewPreviewBusy = false
  clarifyQueue = []
  reviewPreviewTurns = [
    { role: 'agent', text: '请复审 research.json', at: '2026-08-01T00:00:00Z' },
    { role: 'human', text: '请改标题', at: '2026-08-01T00:02:00Z' },
  ]
}

;(api as any).createGateShareLink = async (
  _runId: string,
  _nodeId: string,
  ttlTier = '24h',
  permissionPreset = 'full',
) => ({
  id: 'gsl-e2e',
  url: fixtureShareUrl(),
  ttlTier,
  permissionPreset,
  expiresAt: new Date(Date.now() + 24 * 3600 * 1000).toISOString(),
  state: 'active',
})
;(api as any).regenGateShareLink = async () => ({
  id: 'gsl-e2e-2',
  url: fixtureShareUrl('cd'.repeat(32)),
  ttlTier: '24h',
  permissionPreset: 'full',
  expiresAt: new Date(Date.now() + 24 * 3600 * 1000).toISOString(),
  state: 'active',
})
;(api as any).revokeGateShareLink = async () => ({ status: 'revoked' })
;(api as any).createReviewShareLink = async (
  _runId: string,
  _nodeId: string,
  ttlTier = '24h',
  permissionPreset = 'full',
) => ({
  id: 'gsl-e2e-review',
  url: fixtureShareUrl(),
  ttlTier,
  permissionPreset,
  expiresAt: new Date(Date.now() + 24 * 3600 * 1000).toISOString(),
  state: 'active',
})
;(api as any).regenReviewShareLink = async () => ({
  id: 'gsl-e2e-review-2',
  url: fixtureShareUrl('cd'.repeat(32)),
  ttlTier: '24h',
  permissionPreset: 'full',
  expiresAt: new Date(Date.now() + 24 * 3600 * 1000).toISOString(),
  state: 'active',
})
;(api as any).revokeReviewShareLink = async () => ({ status: 'revoked' })

const originalFetch = window.fetch.bind(window)
window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  if (url.includes('/public/gate-approvals/artifacts') && !url.includes('/content')) {
    return new Response(JSON.stringify({ status: 'active', artifacts: [], nodes: [] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  if (url.includes('/public/gate-approvals/artifacts/') && url.includes('/content')) {
    return new Response(JSON.stringify({ status: 'active', name: 'research.json', content: '{}' }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  if (
    url.includes('/public/gate-approvals/preview') &&
    !url.includes('/preview-ticket') &&
    !url.includes('/preview-vnc') &&
    !url.includes('/preview-api')
  ) {
    const params = new URLSearchParams(location.search)
    const scene = params.get('scene') || ''
    if (params.get('slowPreview')) {
      await new Promise((resolve) => setTimeout(resolve, 800))
    }
    if (scene === 'public-clarify') {
      if (!reviewPreviewTurns.length || reviewPreviewTurns[0]?.text === '请复审 research.json') {
        reviewPreviewTurns = [{ role: 'agent', text: '请补充验收标准', at: '2026-08-01T00:00:00Z' }]
      }
      return new Response(
        JSON.stringify({
          status: clarifyLinkUsed ? 'used' : 'active',
          kind: 'review',
          nodeType: 'react',
          title: '需求澄清',
          description: '外部澄清。请回答问题，信息足够后确认并流转。',
          remainingSec: 3600,
          nonce: 'nonce-e2e-clarify',
          reactSessionAlive: true,
          sessionBusy: reviewPreviewBusy,
          waiting: clarifyQueue.length,
          queueItems: clarifyQueue,
          productKind: '',
          productName: '',
          actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
          turns: reviewPreviewTurns.length
            ? reviewPreviewTurns
            : [{ role: 'agent', text: '请补充验收标准', at: '2026-08-01T00:00:00Z' }],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }
    if (scene === 'public-review') {
      return new Response(
        JSON.stringify({
          status: 'active',
          kind: 'review',
          title: '调研',
          description: '待复审脱敏摘要',
          remainingSec: 3600,
          nonce: 'nonce-e2e-review',
          reactSessionAlive: true,
          sessionBusy: reviewPreviewBusy,
          waiting: reviewPreviewBusy ? 0 : 0,
          activeItem: reviewPreviewBusy ? { text: '请改标题' } : undefined,
          productKind: 'structured',
          productName: 'research.json',
          actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
          structured: { name: 'research.json', title: '调研摘要', doc: { title: '调研摘要' } },
          turns: reviewPreviewTurns,
          upstream: { name: 'clarified_requirement.json', title: '澄清', summary: '已有澄清需求文档，可对照审阅当前主产物' },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }
    if (scene === 'public-app-preview') {
      return new Response(
        JSON.stringify({
          status: 'active',
          kind: 'review',
          title: '应用预览',
          description: '应用预览待审批',
          remainingSec: 3600,
          nonce: 'nonce-e2e-preview',
          reactSessionAlive: true,
          sessionBusy: false,
          waiting: 0,
          productKind: 'app_preview',
          productName: 'app_preview',
          actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
          ports: [
            { port: 5173, label: 'Web · 5173', mode: 'vnc' },
            { port: 8080, label: 'API · 8080', mode: 'vnc' },
          ],
          turns: [{ role: 'agent', text: '应用预览已就绪（set_preview 可达）。公开页可远程与取点。', at: '2026-08-01T00:00:00Z' }],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }
    return new Response(
      JSON.stringify({
        status: 'active',
        kind: 'human_gate',
        title: '审阅视觉稿',
        description: '请审阅脱敏产物',
        remainingSec: 3600,
        nonce: 'nonce-e2e',
        reactSessionAlive: true,
        productKind: 'visual',
        productName: 'page.html',
        actions: { approve: 'approve', reject: 'revise', confirm: 'approve', reply: 'reply', cancel: 'cancel' },
        visualHtml: '<p>ok</p>',
        structured: { title: '外部一次审批', goals: ['g1'] },
        turns: [{ role: 'agent', text: '请审阅 page.html', at: '2026-08-01T00:00:00Z' }],
        upstream: { name: 'clarified_requirement.json', title: '澄清', summary: '已有澄清需求文档，可对照审阅当前主产物' },
        visualHtmlHash: 'e2e-vh',
        upstreamHash: 'e2e-up',
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )
  }
  if (url.includes('/public/gate-approvals/preview-ticket')) {
    let port = 5173
    try {
      const body = typeof init?.body === 'string' ? JSON.parse(init.body) : {}
      if (typeof body.port === 'number') port = body.port
    } catch {
      /* ignore */
    }
    const ticket = `e2e-ticket-${port}`
    return new Response(
      JSON.stringify({
        status: 'active',
        ticket,
        expiresAt: new Date(Date.now() + 120_000).toISOString(),
        port,
        mode: 'vnc',
        wsPath: '/public/gate-approvals/preview-vnc/ws',
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )
  }
  if (url.includes('/public/gate-approvals/upstream')) {
    return new Response(
      JSON.stringify({
        status: 'active',
        upstream: {
          name: 'clarified_requirement.json',
          title: '澄清',
          summary: '已有澄清需求文档，可对照审阅当前主产物',
          doc: { title: '澄清全文', summary: '按需全文', goals: ['三区布局'] },
        },
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )
  }
  if (url.includes('/public/gate-approvals/reply')) {
    reviewPreviewBusy = true
    const body = init?.body ? JSON.parse(String(init.body)) : {}
    const text = String(body.text || '').trim()
    if (text) {
      clarifyQueue = [...clarifyQueue, { id: `q-${clarifyQueue.length + 1}`, text }]
      reviewPreviewTurns = [
        ...reviewPreviewTurns,
        { role: 'human', text, at: new Date().toISOString() },
      ]
    }
    return new Response(JSON.stringify({ status: 'accepted' }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  if (url.includes('/public/gate-approvals/cancel')) {
    reviewPreviewBusy = false
    return new Response(JSON.stringify({ status: 'ok' }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  if (url.includes('/public/gate-approvals/decide')) {
    if (new URLSearchParams(location.search).get('slowDecide')) {
      await new Promise((r) => setTimeout(r, 800))
    }
    const body = init?.body ? JSON.parse(String(init.body)) : {}
    if (body.action === 'confirm') {
      const scene = new URLSearchParams(location.search).get('scene') || ''
      if (scene === 'public-clarify' && new URLSearchParams(location.search).get('unfinishedConfirm') === '1' && !clarifyConfirmUnfinished) {
        clarifyConfirmUnfinished = true
        return new Response(
          JSON.stringify({
            status: 'validation_failed',
            error: 'review_validation_failed',
            message: '澄清尚未结束',
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }
      reviewPreviewBusy = false
      clarifyLinkUsed = true
      return new Response(JSON.stringify({ status: 'confirmed', action: 'confirm' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    const name = String(body.name || '').trim()
    const comment = String(body.comment || '').trim()
    if (!name || !comment) {
      return new Response(JSON.stringify({ error: 'audit_required', message: '请填写姓名与意见后再提交' }), {
        status: 400,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    const status = body.action === 'revise' ? 'rejected' : 'approved'
    return new Response(JSON.stringify({ status, action: body.action }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  return originalFetch(input, init)
}

const gate: GateInboxItem = {
  type: 'gate',
  nodeType: 'human_gate',
  runId: 'run-e2e-share',
  nodeId: 'hg-e2e',
  iteration: 1,
  workflowName: 'wf',
  title: '审阅视觉稿',
  bodyMd: '请审阅',
  actions: [
    { id: 'approve', label: '批准' },
    { id: 'revise', label: '驳回', requireForm: true },
  ],
  requestedAt: '2026-08-01T00:00:00Z',
  shareLink: { state: 'none', canCreate: true, hasPass: true, hasFail: true },
}

const reviewItem: ClarifyInboxItem = {
  type: 'clarify',
  kind: 'review',
  runId: 'run-e2e-review',
  nodeId: 'research-e2e',
  iteration: 1,
  workflowName: 'wf',
  label: '调研',
  done: false,
  requestedAt: '2026-08-01T00:00:00Z',
  updatedAt: '2026-08-01T00:00:00Z',
  shareLink: { state: 'none', canCreate: true },
}

const clarifyItem: ClarifyInboxItem = {
  type: 'clarify',
  kind: 'clarify',
  runId: 'run-e2e-clarify',
  nodeId: 'react-e2e',
  iteration: 1,
  workflowName: 'wf',
  label: '需求澄清',
  done: false,
  requestedAt: '2026-08-01T00:00:00Z',
  updatedAt: '2026-08-01T00:00:00Z',
  shareLink: { state: 'none', canCreate: true },
}

const appPreviewItem: ClarifyInboxItem = {
  type: 'clarify',
  kind: 'app_preview',
  runId: 'run-e2e-preview',
  nodeId: 'app_preview-e2e',
  iteration: 1,
  workflowName: 'wf',
  label: '应用预览',
  done: false,
  requestedAt: '2026-08-01T00:00:00Z',
  updatedAt: '2026-08-01T00:00:00Z',
  shareLink: { state: 'none', canCreate: true },
}

const ToolbarHost = defineComponent({
  name: 'ToolbarHost',
  setup() {
    provide('gateShareOpen', () => {})
    provide('gateShareEnabled', computed(() => true))
    return () =>
      h(HtmlPreview, {
        html: '<p>ok</p>',
        inspectable: true,
        enlargeable: true,
      })
  },
})

const Fixture = defineComponent({
  name: 'GateShareLinkFixture',
  setup() {
    const scene = new URLSearchParams(location.search).get('scene') || 'inbox'
    const open = ref(false)
    const item = ref<InboxItem>(
      scene === 'inbox-review'
        ? { ...reviewItem }
        : scene === 'inbox-app-preview'
          ? { ...appPreviewItem }
          : scene === 'inbox-clarify'
            ? { ...clarifyItem }
            : { ...gate },
    )
    const kind =
      scene === 'inbox-review' || scene === 'inbox-app-preview' || scene === 'inbox-clarify'
        ? 'review'
        : 'human_gate'

    if (
      scene === 'public' ||
      scene === 'public-review' ||
      scene === 'public-app-preview' ||
      scene === 'public-clarify'
    ) {
      if (!location.hash) location.hash = `#t=${TOKEN}`
      return () => h('div', { 'data-testid': 'gate-share-e2e-root' }, [h(PublicGateApprovalView)])
    }

    rememberShareUrl(item.value.runId, item.value.nodeId, item.value.iteration, '')
    return () =>
      h('div', { class: 'mx-auto max-w-md p-4', 'data-testid': 'gate-share-e2e-root' }, [
        h(ToastHost),
        h(InboxPendingCard, {
          item: item.value,
          active: true,
          onSelect: () => {},
          onOpenShare: () => {
            open.value = true
          },
        }),
        scene === 'inbox' ? h(ToolbarHost) : null,
        scene === 'inbox-clarify'
          ? h('div', { class: 'mt-3 flex flex-col gap-2' }, [
              h(
                'button',
                {
                  type: 'button',
                  class: 'text-xs text-accent-2',
                  'data-testid': 'gate-share-copy-btn-detail',
                  onClick: () => {
                    open.value = true
                  },
                },
                '复制临时链接',
              ),
              h(ReviewComposer, {
                mode: 'clarify',
                runId: item.value.runId,
                nodeId: item.value.nodeId,
                iteration: item.value.iteration,
                turns: [],
                done: false,
                active: true,
              }),
            ])
          : null,
        h(GateShareLinkPanel, {
          open: open.value,
          target: {
            runId: item.value.runId,
            nodeId: item.value.nodeId,
            iteration: item.value.iteration,
            shareLink: item.value.shareLink,
            kind,
          },
          kind,
          onClose: () => {
            open.value = false
          },
          onUpdated: (st: GateInboxItem['shareLink']) => {
            item.value = { ...item.value, shareLink: st }
          },
          onRevoked: (st: GateInboxItem['shareLink']) => {
            item.value = { ...item.value, shareLink: st }
            open.value = false
          },
        }),
      ])
  },
})

async function boot() {
  await initLocale()
  await setLocale('zh-CN')
  createApp(Fixture).use(i18n).mount('#app')
}
void boot()
