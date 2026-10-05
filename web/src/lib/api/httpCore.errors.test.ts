// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '@/lib/shared/i18n'
import { req } from './httpCore'

afterEach(() => {
  vi.unstubAllGlobals()
})

function respond(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })))
}

describe('req error mapping', () => {
  it('replaces the operator-facing missing-key error with plain copy', async () => {
    respond(412, { error: 'encrypt credential: 加密主密钥未配置(config: security.secrets_key 或 GRASP_SECRETS_KEY)', code: 'secrets_key_missing' })
    const err = await req('/projects/p1/credentials', { method: 'POST', body: '{}' }).catch((e) => e)
    expect(err).toMatchObject({ status: 412, code: 'secrets_key_missing' })
    expect(err.message).toBe(i18n.global.t('common.errors.secretsKeyMissing'))
    expect(err.message).not.toMatch(/GRASP_|secrets_key/)
  })

  it('keeps the server message for other errors', async () => {
    respond(400, { error: 'bad name' })
    await expect(req('/x', { method: 'POST', body: '{}' })).rejects.toThrow('bad name')
  })
})
