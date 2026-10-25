// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createApp, defineComponent, nextTick } from 'vue'
import common from '@/locales/zh-CN/common.json'
import enCommon from '@/locales/en/common.json'
import {
  WORKFLOW_FAVORITES_MAX,
  favoritesKeyForUser,
  hydrateFromStorage,
  loadFavoriteEntries,
  useWorkflowFavorites,
} from './useWorkflowFavorites'
import { useAuth } from '@/lib/composables/useAuth'
import { useToast } from '@/lib/composables/useToast'

vi.mock('@/lib/api/api', () => ({
  api: {
    getWorkflow: vi.fn(async (id: string) => {
      if (id === 'gone') {
        const err = Object.assign(new Error('missing'), { status: 404 })
        throw err
      }
      return {
        id,
        name: `WF ${id}`,
        description: '',
        status: id === 'draft-1' ? 'draft' : 'published',
        version: 1,
        updatedAt: '',
        needsRepo: false,
        nodes: [],
        edges: [],
      }
    }),
  },
}))

function withSetup<T>(fn: () => T, locale: 'zh-CN' | 'en' = 'zh-CN'): T {
  let result!: T
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: locale === 'en' ? { en: enCommon } : { 'zh-CN': common },
  })
  const Comp = defineComponent({
    setup() {
      result = fn()
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return result
}

function clearToasts() {
  const toast = useToast()
  for (const item of [...toast.toasts.value]) toast.dismiss(item.id)
}

beforeEach(() => {
  localStorage.clear()
  clearToasts()
  const auth = useAuth()
  auth.setUser({ username: 'dev.li', expiresAt: '' })
  hydrateFromStorage()
})

describe('useWorkflowFavorites', () => {
  it('keys storage by username and isolates accounts', () => {
    expect(favoritesKeyForUser('dev.li')).toBe('grasp.workflowFavorites.dev.li')
    const fav = withSetup(() => useWorkflowFavorites())
    fav.toggleFavorite('wf-a', { name: 'A', silent: true })
    expect(loadFavoriteEntries('dev.li')).toHaveLength(1)

    useAuth().setUser({ username: 'other', expiresAt: '' })
    hydrateFromStorage()
    const fav2 = withSetup(() => useWorkflowFavorites())
    expect(fav2.listSorted.value).toHaveLength(0)
    expect(fav2.isFavorite('wf-a')).toBe(false)
  })

  it('keeps new favorites first in the local record and persists that order', async () => {
    vi.useFakeTimers()
    const fav = withSetup(() => useWorkflowFavorites())
    vi.setSystemTime(1000)
    fav.toggleFavorite('wf-a', { silent: true })
    vi.setSystemTime(2000)
    fav.toggleFavorite('wf-b', { silent: true })
    expect(fav.listSorted.value.map((e) => e.workflowId)).toEqual(['wf-b', 'wf-a'])
    fav.reorderFavorites(0, 1)
    expect(fav.listSorted.value.map((e) => e.workflowId)).toEqual(['wf-a', 'wf-b'])
    expect(loadFavoriteEntries('dev.li').map((e) => e.workflowId)).toEqual(['wf-a', 'wf-b'])
    vi.setSystemTime(3000)
    fav.toggleFavorite('wf-c', { silent: true })
    expect(fav.listSorted.value.map((e) => e.workflowId)).toEqual(['wf-c', 'wf-a', 'wf-b'])
    vi.useRealTimers()
  })

  it('hydrates stored array order as-is', () => {
    localStorage.setItem(
      favoritesKeyForUser('dev.li'),
      JSON.stringify([
        { workflowId: 'wf-old', favoritedAt: 1000 },
        { workflowId: 'wf-new', favoritedAt: 2000 },
      ]),
    )
    hydrateFromStorage()
    const fav = withSetup(() => useWorkflowFavorites())
    expect(fav.listSorted.value.map((entry) => entry.workflowId)).toEqual(['wf-old', 'wf-new'])
  })

  it('rejects the 9th favorite without LRU eviction', () => {
    const fav = withSetup(() => useWorkflowFavorites())
    for (let i = 0; i < WORKFLOW_FAVORITES_MAX; i++) {
      expect(fav.toggleFavorite(`wf-${i}`, { silent: true })).toBe(true)
    }
    expect(fav.toggleFavorite('wf-extra', { silent: true })).toBe(false)
    expect(fav.listSorted.value).toHaveLength(WORKFLOW_FAVORITES_MAX)
    expect(fav.isFavorite('wf-extra')).toBe(false)
    // Unfavorite still works when full
    expect(fav.toggleFavorite('wf-0', { silent: true })).toBe(false)
    expect(fav.listSorted.value).toHaveLength(WORKFLOW_FAVORITES_MAX - 1)
  })

  it('does not duplicate on repeated favorite', () => {
    const fav = withSetup(() => useWorkflowFavorites())
    fav.toggleFavorite('wf-a', { silent: true })
    fav.toggleFavorite('wf-a', { silent: true }) // unfavorite
    fav.toggleFavorite('wf-a', { silent: true })
    expect(fav.listSorted.value.filter((e) => e.workflowId === 'wf-a')).toHaveLength(1)
  })

  it('hydrates display names and silently strips 404 favorites', async () => {
    const fav = withSetup(() => useWorkflowFavorites())
    fav.toggleFavorite('wf-ok', { silent: true })
    fav.toggleFavorite('gone', { silent: true })
    await fav.hydrateDisplay()
    await nextTick()
    expect(fav.displayItems.value.map((d) => d.workflowId)).toEqual(['wf-ok'])
    // After strip, only wf-ok remains in storage.
    expect(fav.isFavorite('gone')).toBe(false)
    expect(fav.isFavorite('wf-ok')).toBe(true)
    expect(fav.displayItems.value[0]?.name).toBe('WF wf-ok')
  })

  it('clears to empty after localStorage wipe', () => {
    const fav = withSetup(() => useWorkflowFavorites())
    fav.toggleFavorite('wf-a', { silent: true })
    localStorage.clear()
    hydrateFromStorage()
    expect(fav.listSorted.value).toHaveLength(0)
  })

  it('toasts Chinese favorite, unfavorite, and full-limit copy without sidebar wording', () => {
    const fav = withSetup(() => useWorkflowFavorites())
    const toast = useToast()

    expect(fav.toggleFavorite('wf-a', { name: '自我迭代' })).toBe(true)
    expect(toast.toasts.value.at(-1)).toMatchObject({
      type: 'success',
      message: '已收藏「自我迭代」',
    })
    expect(toast.toasts.value.at(-1)?.message).not.toMatch(/置顶|侧栏/)

    expect(fav.toggleFavorite('wf-a', { name: '自我迭代' })).toBe(false)
    expect(toast.toasts.value.at(-1)).toMatchObject({
      type: 'success',
      message: '已取消收藏「自我迭代」',
    })
    expect(toast.toasts.value.at(-1)?.message).not.toMatch(/置顶|侧栏/)

    for (let i = 0; i < WORKFLOW_FAVORITES_MAX; i++) {
      expect(fav.toggleFavorite(`wf-${i}`, { silent: true })).toBe(true)
    }
    expect(fav.toggleFavorite('wf-extra', { name: '第九个' })).toBe(false)
    expect(fav.isFavorite('wf-extra')).toBe(false)
    expect(fav.listSorted.value).toHaveLength(WORKFLOW_FAVORITES_MAX)
    expect(toast.toasts.value.at(-1)).toMatchObject({
      type: 'warn',
      message: '已达 8 条上限，请先取消其它收藏',
    })
    expect(toast.toasts.value.at(-1)?.message).not.toMatch(/快捷|置顶|侧栏/)
  })

  it('toasts English favorite and full-limit copy without pinned or sidebar', () => {
    const fav = withSetup(() => useWorkflowFavorites(), 'en')
    const toast = useToast()

    fav.toggleFavorite('wf-a', { name: '自我迭代' })
    expect(toast.toasts.value.at(-1)?.message).toBe('Favorited "自我迭代"')
    expect(toast.toasts.value.at(-1)?.message).not.toMatch(/pinned|sidebar/i)

    fav.toggleFavorite('wf-a', { name: '自我迭代' })
    expect(toast.toasts.value.at(-1)?.message).toBe('Removed "自我迭代" from favorites')

    for (let i = 0; i < WORKFLOW_FAVORITES_MAX; i++) {
      fav.toggleFavorite(`wf-${i}`, { silent: true })
    }
    fav.toggleFavorite('wf-extra', { name: 'ninth' })
    expect(toast.toasts.value.at(-1)?.message).toBe('Limit of 8 reached — unfavorite another one first')
    expect(toast.toasts.value.at(-1)?.message).not.toMatch(/shortcut|pinned|sidebar/i)
    expect(fav.isFavorite('wf-extra')).toBe(false)
  })
})
