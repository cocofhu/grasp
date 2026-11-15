import { createApp, defineComponent, h, onMounted, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import '../src/styles/global.css'
import ChatMessageRail from '../src/components/chat/ChatMessageRail.vue'

const items = Array.from({ length: 40 }, (_, i) => ({ id: `q-${i + 1}`, title: `第 ${i + 1} 个提问:如何交付这次变更?`, preview: '先完成验证,再检查交付材料。' }))
const locale = { pages: { chatNavigation: { ariaLabel: '对话定位', jumpTo: '跳转到:{title}', loadedHistory: '仅显示已加载的提问', loadEarlier: '加载更早消息', loadingEarlier: '正在加载更早消息…' } } }

const App = defineComponent(() => {
  const scroller = ref<HTMLElement | null>(null)
  const light = ref(document.documentElement.classList.contains('light'))
  onMounted(() => { scroller.value = document.getElementById('scroller') })
  const toggle = () => { light.value = !light.value; document.documentElement.classList.toggle('light', light.value) }
  return () => h('div', { style: 'position:relative;height:100vh;display:flex;background:rgb(var(--c-base));color:rgb(var(--c-txt))' }, [
    h('button', { id: 'theme-toggle', onClick: toggle, style: 'position:fixed;top:8px;right:8px;z-index:100;padding:4px 10px;border:1px solid rgb(var(--c-line));border-radius:6px' }, light.value ? '切换深色' : '切换浅色'),
    h('div', { style: 'position:relative;width:48px' }, scroller.value ? [h(ChatMessageRail, { items, scroller: scroller.value, scopeKey: 'harness' })] : []),
    h('div', { id: 'scroller', style: 'flex:1;overflow-y:auto;padding:24px' }, items.map(item => h('div', { 'data-chat-message-id': item.id, style: 'height:300px;padding:12px;border-bottom:1px solid rgb(var(--c-line))' }, item.title))),
  ])
})

createApp(App).use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': locale } })).mount('#app')
