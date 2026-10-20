/**
 * Demo-locked Loading/Pending locale parity (专项三).
 * Must not rely only on locales.compile.test — assert zh/en key symmetry and exact copy.
 */
import { describe, expect, it } from 'vitest'
import zhCommon from '@/locales/zh-CN/common.json'
import enCommon from '@/locales/en/common.json'
import zhPages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'

function leafKeys(node: unknown, prefix = ''): string[] {
  if (typeof node === 'string') return [prefix]
  if (!node || typeof node !== 'object' || Array.isArray(node)) return []
  return Object.entries(node as Record<string, unknown>).flatMap(([k, v]) =>
    leafKeys(v, prefix ? `${prefix}.${k}` : k),
  )
}

function atPath(root: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((acc, k) => (acc as Record<string, unknown> | undefined)?.[k], root)
}

describe('loading/pending Demo-locked locale keys', () => {
  const demoCommon: Array<[string, string, string]> = [
    ['buttons.approving', '批准中…', 'Approving…'],
    ['buttons.rejecting', '驳回中…', 'Rejecting…'],
    ['buttons.sending', '发送中…', 'Sending…'],
    ['buttons.submitting', '提交中…', 'Submitting…'],
    ['buttons.refreshing', '正在刷新…', 'Refreshing…'],
    ['buttons.cancelling', '取消中…', 'Cancelling…'],
    ['buttons.copying', '复制中…', 'Copying…'],
    ['buttons.creating', '创建中…', 'Creating…'],
    ['buttons.starting', '启动中…', 'Starting…'],
    ['loading.inProgress', '加载中', 'Loading'],
    ['loading.refreshing', '正在刷新', 'Refreshing'],
    ['loading.retry', '重试', 'Retry'],
    ['loading.timeout', '请求超时', 'Request timed out'],
    ['loading.failed', '加载失败', 'Failed to load'],
    ['loading.label', '加载中', 'Loading'],
    ['loading.stuck', '可能卡死 · 请求仍在进行，可继续等待或重试', 'May be stuck · request still running; wait or retry'],
  ]

  it('zh/en common.buttons + common.loading key trees are symmetric', () => {
    const zhBtn = leafKeys((zhCommon as { common: { buttons: unknown } }).common.buttons)
    const enBtn = leafKeys((enCommon as { common: { buttons: unknown } }).common.buttons)
    expect(zhBtn.sort()).toEqual(enBtn.sort())
    const zhLoad = leafKeys((zhCommon as { common: { loading: unknown } }).common.loading)
    const enLoad = leafKeys((enCommon as { common: { loading: unknown } }).common.loading)
    expect(zhLoad.sort()).toEqual(enLoad.sort())
    expect(zhLoad.sort()).toEqual([
      'failed',
      'inProgress',
      'label',
      'refreshing',
      'retry',
      'stuck',
      'timeout',
    ])
    expect(atPath(zhCommon.common, 'loading.elapsed')).toBeUndefined()
    expect(atPath(enCommon.common, 'loading.elapsed')).toBeUndefined()
  })

  it('locks Demo pending / stuck copy in zh and en', () => {
    for (const [path, zh, en] of demoCommon) {
      expect(atPath(zhCommon.common, path), `zh common.${path}`).toBe(zh)
      expect(atPath(enCommon.common, path), `en common.${path}`).toBe(en)
    }
  })

  it('locks PublicGate loading/submitting and does not leak token/runId copy', () => {
    expect(zhPages.pages.publicGate.loading).toBe('加载中…')
    expect(enPages.pages.publicGate.loading).toBe('Loading…')
    expect(zhPages.pages.publicGate.submitting).toBe('提交中…')
    expect(enPages.pages.publicGate.submitting).toBe('Submitting…')
    expect(zhPages.pages.publicGate.confirming).toBe('正在确认…')
    expect(enPages.pages.publicGate.confirming).toBe('Confirming…')
    expect(zhPages.pages.publicGate.networkError).toBe('网络错误，请稍后重试')
    expect(enPages.pages.publicGate.networkError).toBe('Network error. Please try again.')
    expect(zhPages.pages.publicGate.securityCheckFailed).toBe('安全校验未通过，请再试一次「确认并流转」')
    expect(enPages.pages.publicGate.securityCheckFailed).toMatch(/Security check failed/i)
    expect(zhPages.pages.publicGate.linkInvalid).toBe('链接失效，请重新打开复审链接')
    expect(enPages.pages.publicGate.linkInvalid).toMatch(/no longer valid|reopen/i)
    expect(zhPages.pages.publicGate.networkFault).toBe('网络故障，请检查网络后重试')
    expect(enPages.pages.publicGate.networkFault).toMatch(/Network failure/i)
    expect(zhPages.pages.publicGate.rateLimited).toBe('请求过于频繁，请稍后再试')
    expect(enPages.pages.publicGate.rateLimited).toMatch(/Too many requests/i)
    for (const bag of [zhPages.pages.publicGate, enPages.pages.publicGate]) {
      for (const v of Object.values(bag)) {
        if (typeof v !== 'string') continue
        expect(v).not.toMatch(/run-|token A|no runId/i)
        expect(v).not.toMatch(/\bcsrf\b|\bnonce\b/i)
      }
    }
  })

  it('locks ReAct sending to 发送中…/Sending… and does not retarget preview feedback sending', () => {
    expect(zhPages.pages.gateApproval.reactRevise.sending).toBe('发送中…')
    expect(enPages.pages.gateApproval.reactRevise.sending).toBe('Sending…')
    expect(zhPages.pages.appPreview.feedback.sending).toBe('提交中…')
    expect(enPages.pages.appPreview.feedback.sending).toBe('Submitting…')
  })

  it('zh/en pages.publicGate key trees stay symmetric after new keys', () => {
    const zh = leafKeys(zhPages.pages.publicGate)
    const en = leafKeys(enPages.pages.publicGate)
    expect(zh.sort()).toEqual(en.sort())
  })
})
