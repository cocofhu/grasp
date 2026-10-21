import { createApp, h, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import AnnotationChip from './components/run/AnnotationChip.vue'
import common from './locales/zh-CN/common.json'
import pages from './locales/zh-CN/pages.json'
import type { ReactAnnotation } from './lib/shared/types'
import './styles/global.css'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...pages } },
})

const field: ReactAnnotation = { label: '启动输入', jsonPath: '#launchInput' }
const same: ReactAnnotation = { label: '#launchInput', selector: '#launchInput' }
const quote: ReactAnnotation = {
  label: 'Hero',
  jsonPath: 'summary',
  quote: '这是一段比较长的摘录原文，用来确认单行里超出宽度会省略，悬停仍能看到全文。',
}
const unbound: ReactAnnotation = {
  quote: '未绑定的段落摘录，没有路径。',
  label: '未绑定的段落摘录，没有路径。',
}
const truncated: ReactAnnotation = {
  quote: '这段摘录在发送前被软截断，芯片上不再放徽章。',
  jsonPath: 'body',
  truncated: true,
}

const pending = reactive<ReactAnnotation[]>([
  { ...field },
  { ...quote },
  { ...unbound },
  { ...truncated },
])

function remove(index: number) {
  pending.splice(index, 1)
}

function toggleTheme() {
  document.documentElement.classList.toggle('light')
}

function chipRow(anns: ReactAnnotation[], removable: boolean) {
  return h(
    'div',
    {
      class: removable
        ? 'mb-2 flex flex-wrap gap-1.5'
        : 'flex max-w-[240px] flex-wrap gap-1.5',
      'data-testid': removable ? 'preview-pending' : 'preview-readonly',
    },
    anns.map((ann, i) =>
      h(AnnotationChip, {
        ann,
        removable,
        key: i,
        onRemove: removable ? () => remove(i) : undefined,
      }),
    ),
  )
}

createApp({
  setup() {
    return () =>
      h('div', { class: 'mx-auto grid max-w-5xl gap-6 p-6 md:grid-cols-2' }, [
        h('section', { class: 'rounded-lg border border-line bg-surface p-4', 'data-testid': 'preview-feedback' }, [
          h('h2', { class: 'mb-3 text-sm text-txt2' }, '反馈记录（只读）'),
          h('p', { class: 'mb-3 text-sm leading-relaxed' }, '这一轮把引用改成和项目标签一样的单行芯片。'),
          chipRow([field, quote, unbound, truncated, same], false),
        ]),
        h('section', { class: 'rounded-lg border border-line bg-surface p-4', 'data-testid': 'preview-chat' }, [
          h('div', { class: 'mb-3 flex items-center justify-between gap-2' }, [
            h('h2', { class: 'text-sm text-txt2' }, '澄清对话（待发送可移除）'),
            h(
              'button',
              { type: 'button', class: 'chip', 'data-testid': 'preview-theme', onClick: toggleTheme },
              '切换主题',
            ),
          ]),
          h('div', { class: 'mb-1.5 flex flex-wrap justify-end gap-1.5' }, [
            h(AnnotationChip, { ann: same }),
            h(AnnotationChip, { ann: quote }),
          ]),
          h(
            'div',
            { class: 'mb-4 ml-auto max-w-[80%] rounded-2xl bg-elevated px-3 py-2 text-sm' },
            '已发送的说明',
          ),
          chipRow(pending, true),
          h(
            'div',
            { class: 'rounded-[20px] border border-line bg-base px-3 py-3 text-sm text-txt3' },
            '输入框',
          ),
        ]),
      ])
  },
})
  .use(i18n)
  .mount('#app')
