// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import {
  maskShareUrl,
  parseShareTokenFromHash,
  formatRemainingSec,
  remainingSecFromExpiresAt,
  publicGateContentKey,
  shareStatusLabel,
  canCreateGateShare,
  isGateShareActive,
  rememberShareUrl,
  recallShareUrl,
  forgetShareUrl,
  isHumanGateInboxItem,
  isShareableInboxItem,
  inboxShareKind,
  shareApiErrorMessage,
  isLoopbackHostname,
  isLoopbackShareHost,
} from './gateShareLink'

const t = (key: string, values?: Record<string, unknown>) => {
  if (key.endsWith('remainingDays')) return `${values?.n}d`
  if (key.endsWith('remainingHours')) return `${values?.n}h`
  if (key.endsWith('remainingMinutes')) return `${values?.n}m`
  if (key.endsWith('expired')) return 'expired'
  if (key.endsWith('stateNone')) return 'none'
  if (key.endsWith('stateUsed')) return 'used'
  if (key.endsWith('stateRevoked')) return 'revoked'
  if (key.endsWith('stateExpired')) return 'expired'
  if (key.endsWith('stateActive')) return `active ${values?.remaining}`
  return key
}

describe('gateShareLink helpers', () => {
  it('masks fragment token and parses #t= only', () => {
    const token = 'a'.repeat(64)
    const url = `https://app.example/public/gate-approvals#t=${token}`
    expect(maskShareUrl(url)).toBe('https://app.example/public/gate-approvals#t=••••••••')
    expect(maskShareUrl(url)).not.toContain(token)
    expect(parseShareTokenFromHash(`#t=${token}`)).toBe(token)
    expect(parseShareTokenFromHash('?token=abc')).toBe('')
    expect(parseShareTokenFromHash('')).toBe('')
  })

  it('detects loopback share hosts only (not general LAN)', () => {
    expect(isLoopbackShareHost('http://localhost:8080/public/gate-approvals#t=x')).toBe(true)
    expect(isLoopbackShareHost('http://127.0.0.1:8080/public/gate-approvals#t=x')).toBe(true)
    expect(isLoopbackShareHost('http://[::1]:8080/public/gate-approvals#t=x')).toBe(true)
    expect(isLoopbackHostname('localhost')).toBe(true)
    expect(isLoopbackHostname('::1')).toBe(true)
    expect(isLoopbackShareHost('https://approving.example.com/public/gate-approvals#t=x')).toBe(false)
    expect(isLoopbackShareHost('http://192.168.1.10:8080/public/gate-approvals#t=x')).toBe(false)
    expect(isLoopbackShareHost('http://10.0.0.5/public/gate-approvals#t=x')).toBe(false)
  })

  it('formats remaining time and status chips', () => {
    expect(formatRemainingSec(0, t)).toBe('expired')
    expect(formatRemainingSec(90, t)).toBe('1m')
    expect(formatRemainingSec(3700, t)).toBe('1h')
    expect(formatRemainingSec(2 * 86400, t)).toBe('2d')
    expect(shareStatusLabel({ state: 'none' }, t)).toBe('none')
    expect(shareStatusLabel({ state: 'active', remainingSec: 3600 }, t)).toBe('active 1h')
    expect(shareStatusLabel({ state: 'used' }, t)).toBe('used')
  })

  it('blocks recreate after used; allows after revoke/expired', () => {
    expect(canCreateGateShare({ state: 'used', canCreate: false })).toBe(false)
    expect(canCreateGateShare({ state: 'revoked', canCreate: true })).toBe(true)
    expect(canCreateGateShare({ state: 'expired', canCreate: true })).toBe(true)
    expect(isGateShareActive({ state: 'active' })).toBe(true)
  })

  it('isHumanGateInboxItem only matches human_gate; isShareableInboxItem matches review, app_preview and clarify', () => {
    const humanGate = {
      type: 'gate' as const,
      nodeType: 'human_gate',
      runId: 'r',
      nodeId: 'n',
      workflowName: 'w',
      title: 't',
      bodyMd: '',
      actions: [],
      requestedAt: '',
    }
    expect(isHumanGateInboxItem(humanGate)).toBe(true)
    expect(isShareableInboxItem(humanGate)).toBe(true)
    expect(
      isHumanGateInboxItem({
        type: 'gate',
        nodeType: 'proposal_select',
        runId: 'r',
        nodeId: 'n',
        workflowName: 'w',
        title: 't',
        bodyMd: '',
        actions: [],
        requestedAt: '',
      }),
    ).toBe(false)
    const clarify = {
      type: 'clarify' as const,
      runId: 'r',
      nodeId: 'n',
      workflowName: 'w',
      label: 'l',
      done: false,
      requestedAt: '',
      updatedAt: '',
    }
    expect(isHumanGateInboxItem(clarify)).toBe(false)
    expect(isShareableInboxItem(clarify)).toBe(true)
    expect(inboxShareKind(clarify)).toBe('review')
    expect(isShareableInboxItem({ ...clarify, kind: 'clarify' })).toBe(true)
    expect(inboxShareKind({ ...clarify, kind: 'clarify' })).toBe('review')
    expect(isShareableInboxItem({ ...clarify, kind: 'review' })).toBe(true)
    expect(isShareableInboxItem({ ...clarify, kind: 'app_preview' })).toBe(true)
    expect(inboxShareKind({ ...clarify, kind: 'app_preview' })).toBe('review')
    expect(inboxShareKind({ ...clarify, kind: 'review' })).toBe('review')
    expect(inboxShareKind(humanGate)).toBe('human_gate')
  })

  it('recalls share URL from sessionStorage and maps API error codes', () => {
    const url = 'https://app.example/public/gate-approvals#t=' + 'ab'.repeat(32)
    rememberShareUrl('run-1', 'hg1', 1, url)
    expect(recallShareUrl('run-1', 'hg1', 1)).toBe(url)
    expect(sessionStorage.getItem('grasp.gateShareUrl.run-1:hg1:1')).toBe(url)
    forgetShareUrl('run-1', 'hg1', 1)
    expect(recallShareUrl('run-1', 'hg1', 1)).toBe('')
    sessionStorage.setItem('grasp.gateShareUrl.run-1:hg1:1', url)
    expect(recallShareUrl('run-1', 'hg1', 1)).toBe(url)
    forgetShareUrl('run-1', 'hg1', 1)

    const t = (key: string) => key
    expect(shareApiErrorMessage(new Error('no_standard_action'), t)).toBe(
      'pages.gatesInbox.share.errors.noStandardAction',
    )
    expect(shareApiErrorMessage(new Error('used_readonly'), t)).toBe(
      'pages.gatesInbox.share.errors.usedReadonly',
    )
    expect(shareApiErrorMessage(new Error('review_busy'), t)).toBe(
      'pages.gatesInbox.share.errors.reviewBusy',
    )
  })

  it('mergePublicGatePreview keeps omitted large fields and applies changes', async () => {
    const { mergePublicGatePreview } = await import('./gateShareLink')
    const prev = {
      status: 'active',
      visualHtml: '<p>old</p>',
      visualHtmlHash: 'hash-old',
      upstream: { name: 'clarified_requirement.json', title: '澄清', summary: '旧摘要' },
      upstreamHash: 'up-old',
      remainingSec: 100,
    }
    const same = mergePublicGatePreview(prev, {
      status: 'active',
      visualHtmlHash: 'hash-old',
      upstreamHash: 'up-old',
      remainingSec: 98,
      nonce: 'n2',
    })
    expect(same.visualHtml).toBe('<p>old</p>')
    expect(same.upstream?.summary).toBe('旧摘要')
    expect(same.remainingSec).toBe(98)
    expect(same.nonce).toBe('n2')

    const changed = mergePublicGatePreview(prev, {
      status: 'active',
      visualHtml: '<p>new</p>',
      visualHtmlHash: 'hash-new',
      upstream: { name: 'clarified_requirement.json', title: '澄清', summary: '新摘要' },
      upstreamHash: 'up-new',
      remainingSec: 90,
    })
    expect(changed.visualHtml).toBe('<p>new</p>')
    expect(changed.upstream?.summary).toBe('新摘要')

    const withBodies = {
      ...prev,
      structured: { name: 'research.json', title: '旧' },
      structuredHash: 'st-old',
      turns: [{ role: 'agent', text: '请复审' }],
      turnsHash: 'tn-old',
      nonce: 'keep-me',
    }
    const sparseBodies = mergePublicGatePreview(withBodies, {
      status: 'active',
      structuredHash: 'st-old',
      turnsHash: 'tn-old',
      remainingSec: 10,
    })
    expect(sparseBodies.structured?.title).toBe('旧')
    expect(sparseBodies.turns?.[0]?.text).toBe('请复审')
    expect(sparseBodies.nonce).toBe('keep-me')

    const idle = mergePublicGatePreview(
      { ...prev, sessionBusy: true, liveEvents: [{ kind: 'message', text: '流式正文' }] },
      { status: 'active', sessionBusy: false, remainingSec: 1 },
    )
    expect(idle.liveEvents).toBeUndefined()

    // The server omits false/zero session fields; absent must read as idle.
    const busyPrev = {
      ...prev,
      sessionBusy: true,
      waiting: 2,
      queueItems: [{ id: 'q1', text: '下一条' }],
      activeItem: { id: 'a1', text: '当前' },
    }
    const omitted = mergePublicGatePreview(busyPrev, { status: 'active', remainingSec: 1 })
    expect(omitted.sessionBusy).toBe(false)
    expect(omitted.waiting).toBe(0)
    expect(omitted.queueItems).toBeUndefined()
    expect(omitted.activeItem).toBeUndefined()
  })

  it('remainingSecFromExpiresAt prefers expiresAt over stale remainingSec (plan g2.1)', () => {
    const now = Date.parse('2026-08-11T15:00:00.000Z')
    expect(remainingSecFromExpiresAt('2026-08-11T16:30:00.000Z', 30, now)).toBe(90 * 60)
    expect(remainingSecFromExpiresAt(undefined, 45, now)).toBe(45)
    expect(remainingSecFromExpiresAt('2026-08-11T14:00:00.000Z', 99, now)).toBe(0)
  })

  it('publicGateContentKey ignores clock and nonce (plan g3.1)', () => {
    const base = {
      status: 'active' as const,
      visualHtmlHash: 'vh',
      structuredHash: 'st',
      turnsHash: 'tn',
      upstreamHash: 'up',
      remainingSec: 100,
      expiresAt: '2026-08-11T16:00:00.000Z',
      nonce: 'n1',
      turns: [{ role: 'agent', text: '请复审' }],
    }
    expect(publicGateContentKey({ ...base, remainingSec: 1, nonce: 'n2', expiresAt: '2026-08-11T17:00:00.000Z' })).toBe(
      publicGateContentKey(base),
    )
    expect(publicGateContentKey({ ...base, turnsHash: 'tn-new' })).not.toBe(publicGateContentKey(base))
    expect(publicGateContentKey({ ...base, liveEvents: [{ kind: 'message', text: '流' }] })).not.toBe(
      publicGateContentKey(base),
    )
  })
})
