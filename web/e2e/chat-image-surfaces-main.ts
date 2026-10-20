import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import PmLeaderChat from '../src/components/pm/PmLeaderChat.vue'
import PreviewFeedbackChat from '../src/components/run/PreviewFeedbackChat.vue'
import ParagraphInput from '../src/components/ui/ParagraphInput.vue'
import AgentChatTester from '../src/components/agent/AgentChatTester.vue'
import ChatImageThumb from '../src/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '../src/components/ui/ChatImagePreviewModal.vue'
import { useChatImagePreview } from '../src/lib/composables/useChatImagePreview'
import type { ClarifyImage } from '../src/lib/shared/types'

initLocale()
setTheme('light')

const PNG_A =
  'iVBORw0KGgoAAAANSUhEUgAAAPAAAACgCAIAAAC9uXYyAAABRElEQVR42u3SQQ0AAAgDsQmbfxWIQQUf0qQKLpdp4Y1IgKHB0GBoMDSGBkODocHQYGgMDYYGQ4OhwdAYGgwNhgZDg6ExNBgaDA2GBkNjaDA0GBoMDYbG0GBoMDQYGgyNocHQYGgwNIYGQ4OhwdBgaAwNhgZDg6HB0BgaDA2GBkODoTE0GBoMDYYGQ2NoMDQYGgwNhsbQYGgwNBgaDI2hwdBgaDA0GBpDg6HB0GBoDA2GBkODocHQGBoMDYYGQ4OhMTQYGgwNhgZDY2gwNBgaDA2GxtBgaDA0GBoMjaHB0GBoMDQYGkODocHQYGgMrQKGBkODocHQGBoMDYYGQ4OhMTQYGgwNhgZDY2gwNBgaDA2GxtBgaDA0GBoMjaHB0GBoMDQYGkODocHQYGgwNIYGQ4OhwdAYGgwNhgZDg6ExNBgaDA03FrNxQ6p/RCs5AAAAAElFTkSuQmCC'
const PNG_B =
  'iVBORw0KGgoAAAANSUhEUgAAAPAAAACgCAIAAAC9uXYyAAABRElEQVR42u3SQQ0AAAjEsJODJtQhFRV5SJMqWJaahjciAYYGQ4OhwdAYGgwNhgZDg6ExNBgaDA2GBkNjaDA0GBoMDYbG0GBoMDQYGgyNocHQYGgwNBgaQ4OhwdBgaDA0hgZDg6HB0BgaDA2GBkODoTE0GBoMDYYGQ2NoMDQYGgwNhsbQYGgwNBgaDI2hwdBgaDA0GBpDg6HB0GBoMDSGBkODocHQYGgMDYYGQ4OhMTQYGgwNhgZDY2gwNBgaDA2GxtBgaDA0GBoMjaHB0GBoMDQYGkODocHQYGgwNIYGQ4OhwdBgaAwNhgZDg6ExtAoYGgwNhgZDY2gwNBgaDA2GxtBgaDA0GBoMjaHB0GBoMDQYGkODocHQYGgwNIYGQ4OhwdBgaAwNhgZDg6HB0BgaDA2GBkNjaDA0GBoMDYbG0GBoMDTcWANS9egxNJk8AAAAAElFTkSuQmCC'

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

class MockWS {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  readyState = MockWS.OPEN
  onopen: (() => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  send() {}
  close() {}
  addEventListener(type: string, listener: EventListener) {
    if (type === 'open') queueMicrotask(() => listener(new Event('open')))
  }
  removeEventListener() {}
  constructor() {
    queueMicrotask(() => this.onopen?.())
  }
}
;(window as unknown as { WebSocket: typeof WebSocket }).WebSocket = MockWS as unknown as typeof WebSocket

window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  const method = (init?.method || 'GET').toUpperCase()

  if (url.includes('/pm/threads') && !url.includes('/messages') && !url.includes('/draft') && !url.includes('/sandbox') && method === 'GET') {
    return json({ items: [{ id: 'thr-1', agentAvailable: true, title: '你好' }] })
  }
  if (url.includes('/pm/threads/thr-1/messages') && method === 'GET') {
    return json({
      items: [
        {
          id: 'u1',
          role: 'user',
          content: '看下这两张',
          status: 'ok',
          createdAt: '2026-08-09T00:00:00Z',
          images: [
            { data: PNG_A, mimeType: 'image/png', name: '看板截图.png' },
            { data: PNG_B, mimeType: 'image/png', name: '表单截图.png' },
            { data: 'DOCDATA', mimeType: 'application/pdf', name: '说明.docx' },
          ],
        },
        {
          id: 'a1',
          role: 'assistant',
          content: '已收到截图，可继续核对细节。',
          status: 'ok',
          createdAt: '2026-08-09T00:01:00Z',
        },
      ],
      hasMore: false,
    })
  }
  if (url.includes('/draft')) {
    return json({ draft: null, live: false, hasFinal: false })
  }
  if (url.includes('/sandbox') && method === 'POST') {
    return json({ sandbox: { id: 1, status: 'running' }, preamble: '', thread: { id: 'thr-1', agentAvailable: true, title: '你好' } })
  }
  if (url.includes('/sandboxes/1/eventlog')) {
    return json({
      events: [
        {
          type: 'prompt_begin',
          promptText: '用这张图验证附图对话',
          imageURLs: [`data:image/png;base64,${PNG_A}`],
        },
      ],
    })
  }
  if (/\/sandboxes\/1(\?|$)/.test(url) && method === 'GET') {
    return json({ id: 1, status: 'running', name: 'sbx-1' })
  }
  if (url.includes('/preview-issues') && method === 'GET') {
    return json({
      issues: [
        {
          id: 'i1',
          body: '顶栏过窄，Tab 被裁切',
          status: 'open',
          createdAt: '2026-08-09T00:00:00Z',
          selector: '.project-detail-tabs',
          images: [
            { data: PNG_A, mimeType: 'image/png', name: 'issue附图.png' },
            { data: 'DOC', mimeType: 'application/pdf', name: '说明.pdf' },
          ],
        },
      ],
    })
  }
  return json({})
}

type Surf = 'pm' | 'feedback' | 'para' | 'tester' | 'shared'

const App = defineComponent({
  name: 'ChatImageSurfacesHarness',
  setup() {
    const surf = ref<Surf>('pm')
    const paraImages = ref<ClarifyImage[]>([
      { data: PNG_A, mimeType: 'image/png', name: '门禁附件.png' },
      { data: 'DOC', mimeType: 'application/pdf', name: '审计记录.txt' },
    ])
    const paraText = ref('门禁意见…')
    const fbImages = ref<ClarifyImage[]>([{ data: PNG_B, mimeType: 'image/png', name: '反馈草稿.png' }])
    const fbText = ref('补充反馈…')
    const { preview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()

    const tabs: { id: Surf; label: string }[] = [
      { id: 'pm', label: '项目管理' },
      { id: 'feedback', label: '预览反馈' },
      { id: 'para', label: '段落输入' },
      { id: 'tester', label: 'Agent 调试' },
      { id: 'shared', label: '共享组件' },
    ]

    return () =>
      h('div', { class: 'min-h-screen p-4', 'data-testid': 'chat-image-surfaces-root' }, [
        h('h1', { class: 'mb-3 text-lg font-semibold' }, '聊天缩略图窗口预览 · 多入口验收'),
        h(
          'div',
          { class: 'mb-3 flex flex-wrap gap-2', role: 'tablist' },
          tabs.map((tab) =>
            h(
              'button',
              {
                type: 'button',
                class:
                  surf.value === tab.id
                    ? 'border-b-2 border-accent px-3 py-1.5 text-sm text-txt'
                    : 'px-3 py-1.5 text-sm text-txt3',
                'data-testid': `surf-tab-${tab.id}`,
                onClick: () => {
                  closeChatImagePreview()
                  surf.value = tab.id
                },
              },
              tab.label,
            ),
          ),
        ),
        surf.value === 'pm'
          ? h(
              'div',
              { class: 'h-[640px] border border-line', 'data-testid': 'surf-panel-pm' },
              [
                h(PmLeaderChat, {
                  projectId: 'proj-1',
                  binding: {
                    enabled: true,
                    agentAvailable: true,
                    agentConfigRef: 'agent-1',
                    aclNote: '',
                  },
                }),
              ],
            )
          : null,
        surf.value === 'feedback'
          ? h(
              'div',
              { class: 'max-w-xl', 'data-testid': 'surf-panel-feedback' },
              [
                h(PreviewFeedbackChat, {
                  runId: 'run-1',
                  nodeId: 'preview-1',
                  selector: 'button.surf.active',
                  elementImage: { data: PNG_B, mimeType: 'image/png', name: '元素截图.png' },
                  text: fbText.value,
                  images: fbImages.value,
                  'onUpdate:text': (v: string) => {
                    fbText.value = v
                  },
                  'onUpdate:images': (v: ClarifyImage[]) => {
                    fbImages.value = v
                  },
                }),
              ],
            )
          : null,
        surf.value === 'para'
          ? h('div', { class: 'max-w-xl rounded-md border border-line p-4', 'data-testid': 'surf-panel-para' }, [
              h('p', { class: 'mb-2 text-xs text-txt3' }, '门禁 Inbox · 复审编辑器附件条'),
              h(ParagraphInput, {
                text: paraText.value,
                images: paraImages.value,
                'onUpdate:text': (v: string) => {
                  paraText.value = v
                },
                'onUpdate:images': (v: ClarifyImage[]) => {
                  paraImages.value = v
                },
              }),
            ])
          : null,
        surf.value === 'tester'
          ? h(
              'div',
              { class: 'h-[640px] border border-line', 'data-testid': 'surf-panel-tester' },
              [
                h(AgentChatTester, { profile: 'cursor', attachId: 1, homeProjectId: 'proj-1', embedded: true }),
              ],
            )
          : null,
        surf.value === 'shared'
          ? h('div', { class: 'space-y-4', 'data-testid': 'surf-panel-shared' }, [
              h('div', { class: 'flex flex-wrap items-end gap-3' }, [
                h(ChatImageThumb, {
                  src: `data:image/png;base64,${PNG_A}`,
                  label: '历史.md.png',
                  mode: 'previewable',
                  size: 'md',
                  testId: 'shared-md-thumb',
                  onPreview: () => openChatImagePreview(`data:image/png;base64,${PNG_A}`, '历史.md.png'),
                }),
                h(ChatImageThumb, {
                  src: `data:image/png;base64,${PNG_B}`,
                  label: '草稿.sm.png',
                  mode: 'previewable',
                  size: 'sm',
                  thumbClass: 'rounded-md',
                  testId: 'shared-sm-thumb',
                  onPreview: () => openChatImagePreview(`data:image/png;base64,${PNG_B}`, '草稿.sm.png'),
                }),
                h(ChatImageThumb, {
                  src: `data:image/png;base64,${PNG_A}`,
                  label: '元素.xs.png',
                  mode: 'previewable',
                  size: 'xs',
                  thumbClass: 'rounded',
                  testId: 'shared-xs-thumb',
                  onPreview: () => openChatImagePreview(`data:image/png;base64,${PNG_A}`, '元素.xs.png'),
                }),
                h(ChatImageThumb, {
                  src: `data:image/png;base64,${PNG_B}`,
                  label: '锁定.png',
                  mode: 'locked',
                  size: 'md',
                  thumbClass: 'rounded-md',
                  testId: 'shared-locked-thumb',
                }),
                h(ChatImageThumb, {
                  src: 'http://127.0.0.1:9/missing-preview.png',
                  label: '失效图.png',
                  mode: 'previewable',
                  size: 'sm',
                  testId: 'shared-fail-thumb',
                  onPreview: () => openChatImagePreview('http://127.0.0.1:9/missing-preview.png', '失效图.png'),
                }),
              ]),
              h(ChatImagePreviewModal, {
                open: !!preview.value,
                src: preview.value?.src || '',
                label: preview.value?.label || '',
                testIdPrefix: 'shared-image-preview',
                onClose: closeChatImagePreview,
              }),
            ])
          : null,
      ])
  },
})

createApp(App).use(i18n).mount('#app')
