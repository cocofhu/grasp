import { createApp } from 'vue'
import { createI18n } from 'vue-i18n'
import ClarifyChat from './components/run/ClarifyChat.vue'
import common from './locales/zh-CN/common.json'
import pages from './locales/zh-CN/pages.json'
import './styles/global.css'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...pages } },
})

const demo = (name: string) =>
  `<!doctype html><html><body style="font:16px sans-serif;padding:24px"><h1>${name}</h1></body></html>`

createApp(ClarifyChat, {
  runId: 'run-1',
  nodeId: 'react-1',
  iteration: 1,
  done: false,
  active: true,
  turns: [
    {
      role: 'agent',
      text: '请选择',
      at: '2026-07-18T00:00:00Z',
      questions: [
        {
          id: 'layout3',
          prompt: '三选一',
          allowMultiple: false,
          options: [
            { id: 'a', label: '方案 A', demoHtml: demo('方案 A'), recommended: true },
            { id: 'b', label: '方案 B', demoHtml: demo('方案 B') },
            { id: 'c', label: '方案 C', demoHtml: demo('方案 C') },
          ],
        },
      ],
    },
  ],
}).use(i18n).mount('#app')
