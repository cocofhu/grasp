// @vitest-environment happy-dom
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EMBED_PAGE_PICK_KEY } from '@/lib/inbox/embedChat'
import pages from '@/locales/zh-CN/pages.json'
import ReferencePlusMenu from './ReferencePlusMenu.vue'

enableAutoUnmount(afterEach)

function menu(pick?: () => void, allowReferences = true) {
  return mount(ReferencePlusMenu, {
    props: { open: true, testPrefix: 'plus', allowReferences },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })],
      provide: pick ? { [EMBED_PAGE_PICK_KEY as symbol]: pick } : {},
      stubs: { Icon: true },
    },
  })
}

describe('ReferencePlusMenu preview picking', () => {
  it('offers picking only when the preview host supplies the capability', async () => {
    const normal = menu()
    expect(normal.find('[data-testid="plus-pick"]').exists()).toBe(false)
    expect(normal.findAll('[role="menuitem"]')).toHaveLength(3)
    const pick = vi.fn()
    const preview = menu(pick)
    expect(preview.get('[data-testid="plus-pick"]').text()).toBe('取点')
    await preview.get('[data-testid="plus-pick"]').trigger('click')
    expect(pick).toHaveBeenCalledOnce()
    expect(preview.emitted('pick')).toHaveLength(1)
  })

  it('retains picking and upload when references are unavailable', () => {
    const preview = menu(vi.fn(), false)
    expect(preview.findAll('[role="menuitem"]')).toHaveLength(2)
    expect(preview.get('[data-testid="plus-pick"]').exists()).toBe(true)
    expect(preview.get('[data-testid="plus-upload"]').exists()).toBe(true)
    expect(preview.find('[data-testid="plus-github"]').exists()).toBe(false)
  })
})
