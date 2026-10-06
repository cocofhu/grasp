import { ref } from 'vue'
import { i18n } from './i18n'
import {
  loadLocaleMessages,
  otherLocale,
  prefetchLocale,
  type AppLocale,
} from './loadLocaleMessages'

export type { AppLocale }

export const LOCALE_STORAGE_KEY = 'grasp-locale'
const STORAGE_KEY = LOCALE_STORAGE_KEY

function readLocaleStorage(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

export function detectLocale(): AppLocale {
  const saved = readLocaleStorage()
  if (saved === 'zh-CN' || saved === 'en') return saved
  const lang = (navigator.language || 'zh-CN').toLowerCase()
  if (lang.startsWith('zh')) return 'zh-CN'
  if (lang.startsWith('en')) return 'en'
  return 'zh-CN'
}

/** Public external page: zh* → 简体中文, otherwise English. Does not persist. */
function detectPublicLocale(): AppLocale {
  const lang = (navigator.language || '').toLowerCase()
  if (lang.startsWith('zh')) return 'zh-CN'
  return 'en'
}

export async function applyPublicLocale(): Promise<void> {
  const saved = readLocaleStorage()
  const next = saved === 'zh-CN' || saved === 'en' ? saved : detectPublicLocale()
  const messages = await loadLocaleMessages(next)
  i18n.global.setLocaleMessage(next, messages)
  i18n.global.locale.value = next
  locale.value = next
  applyHtmlLocale(next)
}

export const locale = ref<AppLocale>(detectLocale())
let localeChangeSequence = 0

function applyHtmlLocale(loc: AppLocale) {
  document.documentElement.lang = loc
}

export async function setLocale(next: AppLocale): Promise<void> {
  const sequence = ++localeChangeSequence
  if (locale.value === next && i18n.global.locale.value === next) {
    localStorage.setItem(STORAGE_KEY, next)
    applyHtmlLocale(next)
    return
  }

  const messages = await loadLocaleMessages(next)
  // A newer user selection wins if locale bundles finish loading out of order.
  if (sequence !== localeChangeSequence) return
  i18n.global.setLocaleMessage(next, messages)
  i18n.global.locale.value = next
  locale.value = next
  localStorage.setItem(STORAGE_KEY, next)
  applyHtmlLocale(next)
  prefetchLocale(otherLocale(next))
}

let initPromise: Promise<void> | null = null
/** Last titleKey from router/App; remembered so initLocale can refresh after messages load. */
let currentTitleKey: string | undefined

function translationsReady(titleKey: string | undefined): boolean {
  if (!i18n.global.te('shell.appName')) return false
  if (titleKey && !i18n.global.te(titleKey)) return false
  return true
}

export function initLocale(): Promise<void> {
  if (!initPromise) {
    initPromise = (async () => {
      const sequence = localeChangeSequence
      const initial = detectLocale()
      const messages = await loadLocaleMessages(initial)
      if (sequence !== localeChangeSequence) return
      i18n.global.setLocaleMessage(initial, messages)
      i18n.global.locale.value = initial
      locale.value = initial
      applyHtmlLocale(initial)
      // Cold start: locale string often stays zh-CN, so App.vue watch will not re-run.
      // Force-refresh once messages exist so document.title never sticks on raw keys.
      updateDocumentTitle(currentTitleKey)

      const idle = window.requestIdleCallback ?? ((cb: () => void) => setTimeout(cb, 200))
      idle(() => prefetchLocale(otherLocale(initial)))
    })()
  }
  return initPromise
}

export function updateDocumentTitle(titleKey: string | undefined) {
  currentTitleKey = titleKey
  // Messages may still be empty (mount before initLocale). Keep HTML placeholder;
  // never write raw i18n keys like "route.login · shell.appName" into the tab.
  if (!translationsReady(titleKey)) return
  const appName = i18n.global.t('shell.appName')
  document.title = titleKey ? `${i18n.global.t(titleKey)} · ${appName}` : appName
}

// Side-effect: kick off locale init on module load
void initLocale()
