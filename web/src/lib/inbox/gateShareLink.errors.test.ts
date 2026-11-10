// @vitest-environment happy-dom
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { i18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
import { publicGateApi, publicShareErrorText } from './gateShareLink'

const previous = i18n.global.locale.value
beforeAll(async () => {
  i18n.global.setLocaleMessage('en', (await loadLocaleMessages('en')) as never)
})
afterEach(() => {
  i18n.global.locale.value = previous
  vi.unstubAllGlobals()
})

describe('public share error text', () => {
  it('keeps the server copy in the Chinese UI', () => {
    i18n.global.locale.value = 'zh-CN'
    expect(publicShareErrorText({ error: 'permission_denied', message: '当前链接权限不允许回复' }, '403')).toBe('当前链接权限不允许回复')
  })

  it('translates known codes for other locales and falls back to the server copy', () => {
    i18n.global.locale.value = 'en'
    expect(publicShareErrorText({ error: 'visitors_full', message: '同时对话的访客已满，请稍后再试' }, '429')).toBe('Too many visitors are chatting. Try again shortly.')
    expect(publicShareErrorText({ error: 'some_new_code', message: '新的错误' }, '400')).toBe('新的错误')
    expect(publicShareErrorText({}, '500')).toBe('500')
  })

  it('throws the localized text from public API calls', async () => {
    i18n.global.locale.value = 'en'
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'csrf', message: '请求未通过安全校验' }), { status: 403, headers: { 'Content-Type': 'application/json' } })))
    const error = await publicGateApi.upstream('token').catch((e) => e)
    expect(error).toMatchObject({ status: 403, body: { error: 'csrf' } })
    expect(error.message).toBe('The request failed a security check. Refresh the page and try again.')
  })
})
