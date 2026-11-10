// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
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

  it('translates known error codes outside the Chinese UI and keeps server copy in it', async () => {
    const previous = i18n.global.locale.value
    try {
      i18n.global.setLocaleMessage('en', (await loadLocaleMessages('en')) as never)
      i18n.global.locale.value = 'en'
      respond(403, { error: '请在 Solo 会话中导入代码', code: 'workspace_import_forbidden' })
      const en = await req('/solo/sessions/s/workspace-imports', { method: 'POST', body: '{}' }).catch((e) => e)
      expect(en).toMatchObject({ status: 403, code: 'workspace_import_forbidden' })
      expect(en.message).toBe('Import code from within a Solo session.')
      expect(en.message).not.toMatch(/[\u4e00-\u9fff]/)

      respond(409, { error: '未知错误码', code: 'some_unmapped_code' })
      await expect(req('/x', { method: 'POST', body: '{}' })).rejects.toThrow('未知错误码')

      i18n.global.locale.value = 'zh-CN'
      respond(403, { error: '请在 Solo 会话中导入代码（更具体）', code: 'workspace_import_forbidden' })
      await expect(req('/x', { method: 'POST', body: '{}' })).rejects.toThrow('请在 Solo 会话中导入代码（更具体）')
    } finally {
      i18n.global.locale.value = previous
    }
  })

  it('keeps the server message for other errors', async () => {
    respond(400, { error: 'bad name' })
    await expect(req('/x', { method: 'POST', body: '{}' })).rejects.toThrow('bad name')
  })

  it('preserves the message and receipt code from a non-maintenance 503 body', async () => {
    respond(503, { error: '消息接收结果尚未确认', code: 'message_not_confirmed' })
    const error = await req('/solo/sessions/s/turns', { method: 'POST', body: '{}' }).catch(error => error)
    expect(error).toMatchObject({ status: 503, code: 'message_not_confirmed', message: '消息接收结果尚未确认' })
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
