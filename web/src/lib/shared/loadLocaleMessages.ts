export type AppLocale = 'zh-CN' | 'en'

const cache = new Map<AppLocale, Record<string, unknown>>()

const modules: Record<AppLocale, () => Promise<Record<string, unknown>[]>> = {
  'zh-CN': () =>
    Promise.all([
      import('../../locales/zh-CN/nav.json').then((m) => m.default),
      import('../../locales/zh-CN/route.json').then((m) => m.default),
      import('../../locales/zh-CN/common.json').then((m) => m.default),
      import('../../locales/zh-CN/nodes.json').then((m) => m.default),
      import('../../locales/zh-CN/pages.json').then((m) => m.default),
      import('../../locales/zh-CN/mcp.json').then((m) => m.default),
      import('../../locales/zh-CN/lang.json').then((m) => m.default),
      import('../../locales/zh-CN/shell.json').then((m) => m.default),
      import('../../locales/zh-CN/canvas.json').then((m) => m.default),
    ]),
  en: () =>
    Promise.all([
      import('../../locales/en/nav.json').then((m) => m.default),
      import('../../locales/en/route.json').then((m) => m.default),
      import('../../locales/en/common.json').then((m) => m.default),
      import('../../locales/en/nodes.json').then((m) => m.default),
      import('../../locales/en/pages.json').then((m) => m.default),
      import('../../locales/en/mcp.json').then((m) => m.default),
      import('../../locales/en/lang.json').then((m) => m.default),
      import('../../locales/en/shell.json').then((m) => m.default),
      import('../../locales/en/canvas.json').then((m) => m.default),
    ]),
}

function mergeMessages(parts: Record<string, unknown>[]): Record<string, unknown> {
  return Object.assign({}, ...parts)
}

export async function loadLocaleMessages(locale: AppLocale): Promise<Record<string, unknown>> {
  const cached = cache.get(locale)
  if (cached) return cached

  try {
    const parts = await modules[locale]()
    const messages = mergeMessages(parts)
    cache.set(locale, messages)
    return messages
  } catch {
    if (locale !== 'zh-CN') {
      return loadLocaleMessages('zh-CN')
    }
    return {}
  }
}

export function prefetchLocale(locale: AppLocale): void {
  void loadLocaleMessages(locale)
}

export function otherLocale(locale: AppLocale): AppLocale {
  return locale === 'zh-CN' ? 'en' : 'zh-CN'
}
