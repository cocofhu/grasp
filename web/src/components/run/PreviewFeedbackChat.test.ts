// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import PreviewFeedbackChat from './PreviewFeedbackChat.vue'

const apiMocks = vi.hoisted(() => ({
  listPreviewIssues: vi.fn(),
  createPreviewIssue: vi.fn(),
  deletePreviewIssue: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listPreviewIssues: apiMocks.listPreviewIssues,
      createPreviewIssue: apiMocks.createPreviewIssue,
      deletePreviewIssue: apiMocks.deletePreviewIssue,
    },
  }
})

function mountChat(opts: { compact?: boolean; selector?: string; copyVariant?: 'review' | 'issue' } = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(PreviewFeedbackChat, {
    props: {
      runId: 'run-1',
      nodeId: 'preview-1',
      port: 8080,
      compact: opts.compact ?? false,
      selector: opts.selector ?? '',
      copyVariant: opts.copyVariant ?? 'issue',
    },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        ParagraphInput: false,
        AppModal: {
          props: ['open', 'title', 'width'],
          emits: ['close'],
          template: `
            <div v-if="open" data-testid="preview-feedback-image-preview-modal">
              <div data-testid="preview-feedback-image-preview-title">{{ title }}</div>
              <button type="button" data-testid="preview-feedback-image-preview-close" @click="$emit('close')">×</button>
              <button type="button" data-testid="preview-feedback-image-preview-backdrop" @click="$emit('close')">backdrop</button>
              <slot />
            </div>
          `,
        },
      },
    },
  })
}

describe('PreviewFeedbackChat', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [{ id: 'i1', body: '按钮颜色不对', createdAt: '2026-07-18T00:00:00Z' }],
    })
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'i2',
      body: '新问题',
      createdAt: '2026-07-18T00:01:00Z',
    })
    apiMocks.deletePreviewIssue.mockResolvedValue(undefined)
  })

  it('loads and renders issue history', async () => {
    const wrapper = mountChat()
    await flushPromises()
    expect(apiMocks.listPreviewIssues).toHaveBeenCalledWith('run-1', 'preview-1')
    expect(wrapper.text()).toContain('按钮颜色不对')
    wrapper.unmount()
  })

  it('submits new issue and clears selector', async () => {
    const wrapper = mountChat({ selector: '#btn-submit' })
    await flushPromises()
    const textarea = wrapper.find('textarea')
    expect(textarea.exists()).toBe(true)
    await textarea.setValue('登录页布局错位')
    const sendBtn = wrapper.findAll('button').find((b) => b.text().includes('提交问题'))
    expect(sendBtn).toBeTruthy()
    await sendBtn!.trigger('click')
    await flushPromises()
    expect(apiMocks.createPreviewIssue).toHaveBeenCalled()
    expect(wrapper.emitted('clear-selector')).toBeTruthy()
    expect(wrapper.emitted('issues-changed')).toBeTruthy()
    wrapper.unmount()
  })

  it('does not let an older list response erase a newly created issue', async () => {
    let resolveList!: (value: { issues: { id: string; body: string; createdAt: string }[] }) => void
    apiMocks.listPreviewIssues.mockImplementationOnce(() => new Promise((resolve) => {
      resolveList = resolve
    }))
    const wrapper = mountChat()
    const textarea = wrapper.find('textarea')
    await textarea.setValue('刚创建的问题')
    apiMocks.createPreviewIssue.mockResolvedValueOnce({
      id: 'i-new',
      body: '刚创建的问题',
      createdAt: '2026-07-18T00:02:00Z',
    })
    const sendBtn = wrapper.findAll('button').find((b) => b.text().includes('提交问题'))!
    await sendBtn.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('刚创建的问题')

    resolveList({
      issues: [{ id: 'i-old', body: '旧列表快照', createdAt: '2026-07-18T00:00:00Z' }],
    })
    await flushPromises()
    expect(wrapper.text()).toContain('刚创建的问题')
    wrapper.unmount()
  })

  it('upserts a created issue already included by an earlier list response', async () => {
    let resolveList!: (value: { issues: { id: string; body: string; createdAt: string }[] }) => void
    let resolveCreate!: (value: { id: string; body: string; createdAt: string }) => void
    apiMocks.listPreviewIssues.mockImplementationOnce(() => new Promise((resolve) => {
      resolveList = resolve
    }))
    apiMocks.createPreviewIssue.mockImplementationOnce(() => new Promise((resolve) => {
      resolveCreate = resolve
    }))
    const wrapper = mountChat()
    await wrapper.find('textarea').setValue('并发创建的问题')
    const sendBtn = wrapper.findAll('button').find((b) => b.text().includes('提交问题'))!
    await sendBtn.trigger('click')
    const issue = { id: 'i-race', body: '并发创建的问题', createdAt: '2026-07-18T00:02:00Z' }
    resolveList({ issues: [issue] })
    await flushPromises()
    expect((wrapper.text().match(/并发创建的问题/g) || []).length).toBe(1)

    resolveCreate(issue)
    await flushPromises()
    expect((wrapper.text().match(/并发创建的问题/g) || []).length).toBe(1)
    wrapper.unmount()
  })

  it('requires body+selector+screenshot in requireElement mode', async () => {
    const wrapper = mount(PreviewFeedbackChat, {
      props: {
        runId: 'run-1',
        nodeId: 'gate-1',
        requireElement: true,
        selector: '',
        elementImage: null,
      },
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'zh-CN',
            messages: { 'zh-CN': { ...common, ...pages } },
          }),
        ],
        stubs: { Icon: true, ParagraphInput: false },
      },
    })
    await flushPromises()
    const textarea = wrapper.find('textarea')
    await textarea.setValue('问题正文')
    const sendBtn = wrapper.findAll('button').find((b) => b.text().includes('提交问题'))
    expect(sendBtn!.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('点选')
    await wrapper.setProps({
      selector: 'h1.title',
      elementImage: { data: 'abc', mimeType: 'image/png' },
    })
    await flushPromises()
    expect(sendBtn!.attributes('disabled')).toBeUndefined()
    await sendBtn!.trigger('click')
    await flushPromises()
    expect(apiMocks.createPreviewIssue).toHaveBeenCalledWith(
      'run-1',
      'gate-1',
      '问题正文',
      'h1.title',
      0,
      [{ data: 'abc', mimeType: 'image/png' }],
    )
    wrapper.unmount()
  })

  it('toggles history in compact mode', async () => {
    const wrapper = mountChat({ compact: true })
    await flushPromises()
    const toggle = wrapper.findAll('button').find((b) => b.text().includes('历史') || b.text().includes('反馈'))
    expect(toggle).toBeTruthy()
    await toggle!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('按钮颜色不对')
    wrapper.unmount()
  })

  it('uses review copy variant in gate context', async () => {
    const wrapper = mountChat({ copyVariant: 'review' })
    await flushPromises()
    expect(wrapper.text()).toContain('评审意见')
    expect(wrapper.text()).not.toContain('问题反馈')
    const sendBtn = wrapper.findAll('button').find((b) => b.text().includes('提交评审意见'))
    expect(sendBtn).toBeTruthy()
    wrapper.unmount()
  })

  it('issue images and element screenshot open single preview without dropping selector', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'i1',
          body: '顶栏过窄',
          createdAt: '2026-07-18T00:00:00Z',
          images: [
            { data: 'ISSUEIMG', mimeType: 'image/png', name: 'issue附图.png' },
            { data: 'DOC', mimeType: 'application/pdf', name: '说明.pdf' },
          ],
        },
      ],
    })
    const wrapper = mount(PreviewFeedbackChat, {
      props: {
        runId: 'run-1',
        nodeId: 'preview-1',
        selector: 'button.surf.active',
        elementImage: { data: 'ELEMIMG', mimeType: 'image/png', name: '元素截图.png' },
        images: [{ data: 'DRAFTFB', mimeType: 'image/png', name: '反馈草稿.png' }],
      },
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'zh-CN',
            messages: { 'zh-CN': { ...common, ...pages } },
          }),
        ],
        stubs: {
          Icon: true,
          ParagraphInput: false,
          AppModal: {
            props: ['open', 'title', 'width'],
            emits: ['close'],
            template: `
              <div v-if="open" data-testid="preview-feedback-image-preview-modal">
                <div data-testid="preview-feedback-image-preview-title">{{ title }}</div>
                <button type="button" data-testid="preview-feedback-image-preview-close" @click="$emit('close')">×</button>
                <button type="button" data-testid="preview-feedback-image-preview-backdrop" @click="$emit('close')">backdrop</button>
                <slot />
              </div>
            `,
          },
        },
      },
    })
    await flushPromises()

    const issueThumb = wrapper.find('[data-testid="preview-issue-image-thumb"]')
    expect(issueThumb.exists()).toBe(true)
    expect(issueThumb.text()).toContain('点击放大')
    expect(issueThumb.text()).not.toContain('不可预览')
    await issueThumb.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="preview-feedback-image-preview-title"]').text()).toBe(
      '图片预览 · issue附图.png',
    )
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('[data-testid="preview-feedback-image-preview-modal"]').exists()).toBe(true)
    await wrapper.find('[data-testid="preview-feedback-image-preview-close"]').trigger('click')
    await flushPromises()

    const elemThumb = wrapper.find('[data-testid="preview-element-image-thumb"]')
    expect(elemThumb.exists()).toBe(true)
    await elemThumb.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="preview-feedback-image-preview-title"]').text()).toBe(
      '图片预览 · 元素截图.png',
    )
    await wrapper.find('[data-testid="preview-feedback-image-preview-img"]').trigger('error')
    await flushPromises()
    expect(wrapper.find('[data-testid="preview-feedback-image-preview-failed"]').text()).toContain(
      '图片加载失败',
    )
    await wrapper.find('[data-testid="preview-feedback-image-preview-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('button.surf.active')
    expect(wrapper.find('[data-testid="preview-element-image-thumb"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="paragraph-draft-image-thumb"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="paragraph-draft-image-thumb"]').text()).not.toContain('不可预览')
    wrapper.unmount()
  })
})
