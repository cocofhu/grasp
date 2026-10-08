// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import {
  beginAutoLoad,
  isKnownMissing,
  markMissing,
  resetBlobMissingCacheForTests,
} from '@/lib/shared/blobMissingCache'
import ChatImageThumb from './ChatImageThumb.vue'

beforeEach(() => {
  resetBlobMissingCacheForTests()
})

function mountThumb(props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common } },
  })
  return mount(ChatImageThumb, {
    props: {
      src: 'data:image/png;base64,AAA',
      label: '看板截图.png',
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

describe('ChatImageThumb', () => {
  it('previewable: hover hint, size md, emits preview', async () => {
    const wrapper = mountThumb({ mode: 'previewable', size: 'md', testId: 'thumb-md' })
    const btn = wrapper.find('[data-testid="thumb-md"]')
    expect(btn.element.tagName).toBe('BUTTON')
    expect(btn.classes().join(' ')).toMatch(/h-20/)
    expect(btn.classes().join(' ')).toMatch(/w-20/)
    expect(wrapper.get('img').classes()).toContain('object-cover')
    expect(btn.classes().join(' ')).toMatch(/hover:border-accent/)
    expect(btn.classes().join(' ')).toMatch(/cursor-pointer/)
    expect(btn.text()).toContain('点击放大')
    expect(btn.text()).not.toContain('不可预览')
    expect(btn.attributes('aria-label')).toBe('预览看板截图.png')
    await btn.trigger('click')
    expect(wrapper.emitted('preview')).toHaveLength(1)
    wrapper.unmount()
  })

  it('locked: unavailable overlay, no preview emit, sm/xs sizes', async () => {
    const locked = mountThumb({ mode: 'locked', size: 'sm', testId: 'thumb-locked' })
    const el = locked.find('[data-testid="thumb-locked"]')
    expect(el.element.tagName).toBe('DIV')
    expect(el.classes().join(' ')).toMatch(/h-14/)
    expect(el.text()).toContain('不可预览')
    expect(el.text()).not.toContain('点击放大')
    await el.trigger('click')
    expect(locked.emitted('preview')).toBeUndefined()
    locked.unmount()

    const xs = mountThumb({ mode: 'previewable', size: 'xs', testId: 'thumb-xs' })
    expect(xs.find('[data-testid="thumb-xs"]').classes().join(' ')).toMatch(/h-8/)
    expect(xs.find('[data-testid="thumb-xs"]').classes().join(' ')).toMatch(/w-8/)
    xs.unmount()
  })

  it('contain fit keeps the whole frame instead of a square crop', () => {
    const wrapper = mountThumb({ fit: 'contain', testId: 'thumb-fit' })
    const btn = wrapper.get('[data-testid="thumb-fit"]')
    expect(btn.classes().join(' ')).not.toMatch(/\bh-20\b/)
    expect(btn.classes().join(' ')).not.toMatch(/\bw-20\b/)
    const img = wrapper.get('img')
    expect(img.classes()).toContain('object-contain')
    expect(img.classes()).not.toContain('object-cover')
    wrapper.unmount()
  })

  it('forwards thumbClass for surface corner rules', () => {
    const wrapper = mountThumb({ thumbClass: 'rounded-md', testId: 'thumb-round' })
    expect(wrapper.find('[data-testid="thumb-round"]').classes()).toContain('rounded-md')
    wrapper.unmount()
  })

  it('load error: placeholder + label + retry; does not emit preview (g3.1/g3.3)', async () => {
    const wrapper = mountThumb({ mode: 'previewable', size: 'md', testId: 'thumb-md', label: '70345B2BE' })
    await wrapper.find('img').trigger('error')
    expect(wrapper.text()).toContain('图片加载失败')
    expect(wrapper.text()).toContain('70345B2BE')
    expect(wrapper.text()).toContain('重试')
    expect(wrapper.find('[data-image-failed="1"]').exists()).toBe(true)
    await wrapper.find('[data-testid="thumb-md"]').trigger('click')
    expect(wrapper.emitted('preview')).toBeUndefined()
    wrapper.unmount()
  })

  it('retry adds cache-buster and restores thumb without QQ re-download (g3.2)', async () => {
    const wrapper = mountThumb({
      mode: 'previewable',
      src: '/api/blobs/abc123',
      label: '70345B2BE',
      testId: 'thumb-retry',
    })
    await wrapper.find('img').trigger('error')
    expect(isKnownMissing('abc123')).toBe(true)
    await wrapper.find('[data-testid="thumb-retry-retry"]').trigger('click')
    expect(isKnownMissing('abc123')).toBe(false)
    const img = wrapper.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toMatch(/\/api\/blobs\/abc123\?_r=1$/)
    await img.trigger('load')
    expect(wrapper.text()).toContain('点击放大')
    expect(wrapper.text()).not.toContain('图片加载失败')
    await wrapper.find('[data-testid="thumb-retry"]').trigger('click')
    expect(wrapper.emitted('preview')).toHaveLength(1)
    wrapper.unmount()
  })

  it('knownMissing on mount: placeholder without img GET (g3.1 / g4.1)', () => {
    markMissing('deadbeef01')
    const wrapper = mountThumb({
      src: '/api/blobs/deadbeef01',
      label: 'orphan.png',
      testId: 'thumb-miss',
    })
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('图片加载失败')
    expect(wrapper.text()).toContain('重试')
    wrapper.unmount()
  })

  it('subscribe: markLoaded remounts thumb when peer retry succeeds (g3.3)', async () => {
    markMissing('sync001')
    const wrapper = mountThumb({
      src: '/api/blobs/sync001',
      label: 'sync.png',
      testId: 'thumb-sync',
    })
    expect(wrapper.find('img').exists()).toBe(false)
    const { markLoaded } = await import('@/lib/shared/blobMissingCache')
    markLoaded('sync001')
    await wrapper.vm.$nextTick()
    expect(wrapper.find('img').exists()).toBe(true)
    await wrapper.find('img').trigger('load')
    expect(wrapper.text()).toContain('点击放大')
    wrapper.unmount()
  })

  it('subscribe: peer markMissing(other id) must not open blocked_pending img (g1.2)', async () => {
    const id = '5b32f70529a64bdebafade19ca497a35'
    // Strip (or peer) already owns the single auto GET slot.
    expect(beginAutoLoad(id)).toBe('proceed')
    const wrapper = mountThumb({
      src: `/api/blobs/${id}`,
      label: 'orphan-b.png',
      testId: 'thumb-race',
    })
    // Thumb lost the race → blocked_pending → no requesting img.
    expect(wrapper.find('img').exists()).toBe(false)
    // Unrelated orphan finishes → notify must NOT flip allowImg for this id.
    markMissing('e54381fb9ce8471dbe0765d99fc0239f')
    await wrapper.vm.$nextTick()
    expect(wrapper.find('img').exists()).toBe(false)
    // Same-id peer failure → placeholder, still no second auto GET.
    markMissing(id)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('图片加载失败')
    wrapper.unmount()
  })
})
