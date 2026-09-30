import '../src/styles/global.css'
import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import EmbedNodeChatView from '../src/views/EmbedNodeChatView.vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'

// Mount the production drawer: capability fetching, ticket redemption, public
// Chat, WebSocket sessions and postMessage all follow the shipping code path.
initLocale()
setTheme('dark')
const router = createRouter({
  history: createWebHistory(),
  routes: [{ path: '/embed/runs/:runId/nodes/:nodeId/chat', component: EmbedNodeChatView }],
})
const app = createApp(EmbedNodeChatView).use(i18n).use(router)
void router.isReady().then(() => app.mount('#app'))
