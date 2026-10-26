// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import ParagraphInput from './ParagraphInput.vue'

const PreviewAppModalStub = {
  props: ['open', 'title', 'width'],
  emits: ['close'],
  template: `
    <div v-if="open" data-testid="paragraph-image-preview-modal">
      <div data-testid="paragraph-image-preview-title">{{ title }}</div>
      <button type="button" data-testid="paragraph-image-preview-close" @click="$emit('close')">×</button>
      <button type="button" data-testid="paragraph-image-preview-backdrop" @click="$emit('close')">backdrop</button>
      <slot />
    </div>
  `,
}

function mountInput(textOnly = false, images: { data: string; mimeType: string; name?: string }[] = []) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common } },
  })
  return mount(ParagraphInput, {
    props: { text: 'hello', textOnly, images },
    global: { plugins: [i18n], stubs: { Icon: true, AppModal: PreviewAppModalStub } },
  })
}

describe('ParagraphInput', () => {
  it('renders textarea with initial text', () => {
    const wrapper = mountInput(true)
    const ta = wrapper.find('textarea')
    expect(ta.exists()).toBe(true)
    expect((ta.element as HTMLTextAreaElement).value).toBe('hello')
    wrapper.unmount()
  })

  it('updates text model on input', async () => {
    const wrapper = mountInput(true)
    await wrapper.find('textarea').setValue('updated')
    expect(wrapper.emitted('update:text')?.[0]).toEqual(['updated'])
    wrapper.unmount()
  })

  it('shows attach control when not text-only', () => {
    const wrapper = mountInput(false)
    expect(wrapper.find('[data-testid="paragraph-input-attach"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="paragraph-input-attach"]').attributes('title')).toBe('添加附件')
    expect(wrapper.find('[data-testid="paragraph-input"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="paragraph-input-root"]').attributes('data-text-only')).toBe('0')
    wrapper.unmount()
  })

  it('hides attach control when text-only', () => {
    const wrapper = mountInput(true)
    expect(wrapper.find('[data-testid="paragraph-input-attach"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="paragraph-input-root"]').attributes('data-text-only')).toBe('1')
    wrapper.unmount()
  })

  it('removes only the clicked pending attachment and disables without a red class', async () => {
    const wrapper = mountInput(false, [
      { data: 'AAA', mimeType: 'image/png', name: 'a.png' },
      { data: 'BBB', mimeType: 'application/pdf', name: 'b.pdf' },
    ])
    const buttons = wrapper.findAll('button.pending-attach-remove')
    expect(buttons).toHaveLength(2)
    for (const button of buttons) {
      expect(button.classes()).toContain('rounded-full')
      expect(button.classes()).not.toContain('bg-err')
      expect(button.attributes('disabled')).toBeUndefined()
    }
    await buttons[0].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="paragraph-draft-image-thumb"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="paragraph-pending-file-chip"]').text()).toContain('b.pdf')
    expect(wrapper.findAll('button.pending-attach-remove')).toHaveLength(1)

    const disabled = mount(ParagraphInput, {
      props: {
        text: 'hello',
        disabled: true,
        images: [{ data: 'AAA', mimeType: 'image/png', name: 'a.png' }],
      },
      global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common } } })], stubs: { Icon: true, AppModal: PreviewAppModalStub } },
    })
    const locked = disabled.get('button.pending-attach-remove')
    expect((locked.element as HTMLButtonElement).disabled).toBe(true)
    expect(locked.classes()).not.toContain('bg-err')
    disabled.unmount()
    wrapper.unmount()
  })

  it('hides thumbs in text-only even if images are passed', () => {
    const wrapper = mountInput(true, [{ data: 'AAA', mimeType: 'image/png', name: 'a.png' }])
    expect(wrapper.find('[data-testid="paragraph-draft-image-thumb"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('opens draft image preview, Esc stays, close keeps attachment, no unavailable overlay', async () => {
    const wrapper = mountInput(false, [
      { data: 'DRAFTPARA', mimeType: 'image/png', name: '门禁附件.png' },
      { data: 'DOC', mimeType: 'application/pdf', name: '审计记录.txt' },
    ])
    const thumb = wrapper.find('[data-testid="paragraph-draft-image-thumb"]')
    expect(thumb.exists()).toBe(true)
    expect(thumb.text()).toContain('点击放大')
    expect(thumb.text()).not.toContain('不可预览')
    expect(wrapper.find('[data-testid="paragraph-pending-file-chip"]').exists()).toBe(true)

    await thumb.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="paragraph-image-preview-title"]').text()).toBe('图片预览 · 门禁附件.png')
    expect(wrapper.find('[data-testid="paragraph-image-preview-prev"]').exists()).toBe(false)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('[data-testid="paragraph-image-preview-modal"]').exists()).toBe(true)

    await wrapper.find('[data-testid="paragraph-image-preview-img"]').trigger('error')
    await flushPromises()
    expect(wrapper.find('[data-testid="paragraph-image-preview-failed"]').text()).toContain('图片加载失败')
    await wrapper.find('[data-testid="paragraph-image-preview-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="paragraph-image-preview-modal"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="paragraph-draft-image-thumb"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('embedded layout keeps multiline textarea and hides side attach (g1.1)', () => {
    const wrapper = mount(ParagraphInput, {
      props: { text: 'a\nb', textOnly: false, embedded: true },
      global: {
        plugins: [
          createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common } } }),
        ],
        stubs: { Icon: true, AppModal: PreviewAppModalStub },
      },
    })
    expect(wrapper.find('[data-testid="paragraph-input-attach"]').exists()).toBe(false)
    const ta = wrapper.find('[data-testid="paragraph-input"]').element as HTMLTextAreaElement
    expect(ta.tagName).toBe('TEXTAREA')
    expect(ta.value).toContain('\n')
    expect(typeof (wrapper.vm as { pickFiles?: () => void }).pickFiles).toBe('function')
    wrapper.unmount()
  })

  it('compact embedded starts at two lines so the mobile drawer keeps room (g2.2)', () => {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common } } })
    const stubs = { Icon: true, AppModal: PreviewAppModalStub }
    const roomy = mount(ParagraphInput, {
      props: { text: '', textOnly: false, embedded: true },
      global: { plugins: [i18n], stubs },
    })
    const compact = mount(ParagraphInput, {
      props: { text: '', textOnly: false, embedded: true, compact: true },
      global: { plugins: [i18n], stubs },
    })
    expect(roomy.find('[data-testid="paragraph-input"]').classes()).toContain('min-h-[72px]')
    expect(compact.find('[data-testid="paragraph-input"]').classes()).toContain('min-h-[40px]')
    expect(compact.find('[data-testid="paragraph-input"]').attributes('rows')).toBe('2')
    roomy.unmount()
    compact.unmount()
  })
})
