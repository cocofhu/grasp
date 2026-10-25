// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '@/lib/shared/i18n'
import { isClarifyVisitEndedError, req } from './httpCore'

afterEach(() => {
  vi.unstubAllGlobals()
})

function respond(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })))
}

describe('req error mapping', () => {
  it('replaces the operator-facing missing-key error with plain copy', async () => {
    respond(412, { error: 'encrypt credential: 加密主密钥未配置(config: security.secrets_key 或 GRASP_SECRETS_KEY)', code: 'secrets_key_missing' })
    const err = await req('/workspace/credentials', { method: 'POST', body: '{}' }).catch((e) => e)
    expect(err).toMatchObject({ status: 412, code: 'secrets_key_missing' })
    expect(err.message).toBe(i18n.global.t('common.errors.secretsKeyMissing'))
    expect(err.message).not.toMatch(/GRASP_|secrets_key/)
  })

  it('keeps the server message for other errors', async () => {
    respond(400, { error: 'bad name' })
    await expect(req('/x', { method: 'POST', body: '{}' })).rejects.toThrow('bad name')
  })

  it.each([true, false])('passes clarify_visit_ended autoRetryOnResume=%s through', async (autoRetryOnResume) => {
    respond(409, { error: '运行已结束，这条消息没有发送', code: 'clarify_visit_ended', autoRetryOnResume })
    const err = await req('/runs/r/nodes/n/reply', { method: 'POST', body: '{}' }).catch((e) => e)
    expect(err).toMatchObject({ status: 409, code: 'clarify_visit_ended', autoRetryOnResume })
    expect(err.message).toBe('运行已结束，这条消息没有发送')
    expect(isClarifyVisitEndedError(err)).toBe(true)
  })

  it('leaves autoRetryOnResume off other errors', async () => {
    respond(409, { error: 'busy', code: 'sandbox_busy', runningOpId: 'op-1' })
    const err = await req('/x', { method: 'POST', body: '{}' }).catch((e) => e)
    expect(err).toMatchObject({ code: 'sandbox_busy', runningOpId: 'op-1' })
    expect('autoRetryOnResume' in err).toBe(false)
    expect(isClarifyVisitEndedError(err)).toBe(false)
    expect(isClarifyVisitEndedError(null)).toBe(false)
  })
})
