// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'

type MsgTree = Record<string, unknown>

const { catalogs, resolvePath } = vi.hoisted(() => {
  function resolvePath(tree: MsgTree | undefined, path: string): unknown {
    if (!tree) return undefined
    return path.split('.').reduce<unknown>((node, part) => {
      if (node && typeof node === 'object' && part in (node as MsgTree)) {
        return (node as MsgTree)[part]
      }
      return undefined
    }, tree)
  }

  const catalogs: Record<string, MsgTree> = {
    'zh-CN': {
      shell: { appName: 'Grasp' },
      route: { login: '登录', runs: '运行记录' },
    },
    en: {
      shell: { appName: 'Grasp' },
      route: { login: 'Login', runs: 'Run history' },
    },
  }
  return { catalogs, resolvePath }
})

vi.mock('./loadLocaleMessages', () => ({
  loadLocaleMessages: vi.fn(async (loc: string) => catalogs[loc] ?? catalogs['zh-CN']),
  otherLocale: (loc: string) => (loc === 'en' ? 'zh-CN' : 'en'),
  prefetchLocale: vi.fn(),
}))

vi.mock('./i18n', () => {
  const locale = { value: 'zh-CN' }
  let messages: MsgTree = {}
  return {
    i18n: {
      global: {
        locale,
        setLocaleMessage: vi.fn((_loc: string, next: MsgTree) => {
          messages = next
        }),
        te: (key: string) => typeof resolvePath(messages, key) === 'string',
        t: (key: string) => {
          const hit = resolvePath(messages, key)
          return typeof hit === 'string' ? hit : key
        },
      },
    },
  }
})

import { applyPublicLocale, detectLocale, initLocale, setLocale, updateDocumentTitle, locale } from './locale'
import { loadLocaleMessages, prefetchLocale } from './loadLocaleMessages'
import { i18n } from './i18n'

describe('locale', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.lang = ''
    document.title = 'Grasp · 开发工作流编排'
    vi.mocked(loadLocaleMessages).mockClear()
    vi.mocked(prefetchLocale).mockClear()
    vi.mocked(i18n.global.setLocaleMessage).mockClear()
    // Reset message catalog so each case can simulate empty → loaded.
    i18n.global.setLocaleMessage('zh-CN', {})
  })

  it('detectLocale prefers stored then navigator language', () => {
    localStorage.setItem('grasp-locale', 'en')
    expect(detectLocale()).toBe('en')
    localStorage.setItem('grasp-locale', 'zh-CN')
    expect(detectLocale()).toBe('zh-CN')
    localStorage.removeItem('grasp-locale')
    Object.defineProperty(navigator, 'language', { configurable: true, value: 'en-US' })
    expect(detectLocale()).toBe('en')
    Object.defineProperty(navigator, 'language', { configurable: true, value: 'zh-TW' })
    expect(detectLocale()).toBe('zh-CN')
    Object.defineProperty(navigator, 'language', { configurable: true, value: 'fr-FR' })
    expect(detectLocale()).toBe('zh-CN')
  })

  it('setLocale loads messages and updates document lang', async () => {
    // Ensure we are not already on the target locale (module init may detect en).
    await setLocale('zh-CN')
    vi.mocked(prefetchLocale).mockClear()
    await setLocale('en')
    expect(locale.value).toBe('en')
    expect(localStorage.getItem('grasp-locale')).toBe('en')
    expect(document.documentElement.lang).toBe('en')
    expect(prefetchLocale).toHaveBeenCalledWith('zh-CN')

    await setLocale('en')
    expect(document.documentElement.lang).toBe('en')
  })

  it('public locale preserves a saved choice before browser detection', async () => {
    Object.defineProperty(navigator, 'language', { configurable: true, value: 'en-US' })
    localStorage.setItem('grasp-locale', 'zh-CN')
    await applyPublicLocale()
    expect(locale.value).toBe('zh-CN')
    expect(document.documentElement.lang).toBe('zh-CN')

    localStorage.removeItem('grasp-locale')
    await applyPublicLocale()
    expect(locale.value).toBe('en')
    expect(document.documentElement.lang).toBe('en')
  })

  it('updateDocumentTitle skips writing i18n keys when messages are empty', () => {
    document.title = 'Grasp · 开发工作流编排'
    updateDocumentTitle('route.login')
    expect(document.title).toBe('Grasp · 开发工作流编排')
    expect(document.title).not.toContain('route.login')
    expect(document.title).not.toContain('shell.appName')
  })

  it('updateDocumentTitle writes translated login title after messages load', () => {
    i18n.global.setLocaleMessage('zh-CN', catalogs['zh-CN'])
    updateDocumentTitle('route.login')
    expect(document.title).toBe('登录 · Grasp')

    i18n.global.setLocaleMessage('en', catalogs.en)
    i18n.global.locale.value = 'en'
    updateDocumentTitle('route.login')
    expect(document.title).toBe('Login · Grasp')

    updateDocumentTitle('route.runs')
    expect(document.title).toBe('Run history · Grasp')
  })

  it('initLocale refreshes the remembered title even when locale string is unchanged', async () => {
    // Gate the bundle load so we can simulate afterEach before messages arrive.
    let release!: (v: MsgTree) => void
    const gate = new Promise<MsgTree>((resolve) => {
      release = resolve
    })
    vi.mocked(loadLocaleMessages).mockImplementationOnce(() => gate)

    vi.resetModules()
    const cold = await import('./locale')
    document.title = 'Grasp · 开发工作流编排'
    cold.updateDocumentTitle('route.login')
    expect(document.title).toBe('Grasp · 开发工作流编排')
    expect(document.title).not.toContain('route.login')
    expect(document.title).not.toContain('shell.appName')

    const before = cold.locale.value
    release(catalogs['zh-CN'])
    await cold.initLocale()
    expect(cold.locale.value).toBe(before)
    expect(document.title).toBe('登录 · Grasp')
    expect(document.title).not.toContain('route.login')
    expect(document.title).not.toContain('shell.appName')
  })

  it('initLocale is idempotent', async () => {
    const p1 = initLocale()
    const p2 = initLocale()
    expect(p1).toBe(p2)
    await p1
  })
})
