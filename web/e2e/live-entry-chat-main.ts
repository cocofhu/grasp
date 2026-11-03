import '../src/styles/global.css'
import { createApp } from 'vue'
import { createRouter, createWebHistory, RouterView } from 'vue-router'
import EmbedNodeChatView from '../src/views/EmbedNodeChatView.vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { vHoverInk } from '../src/lib/shared/hoverInkDirective'

// Mount the production drawer: capability fetching, ticket redemption, public
// Chat, WebSocket sessions and postMessage all follow the shipping code path.
initLocale()
setTheme('dark')
const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/embed/runs/:runId/nodes/:nodeId/chat', component: EmbedNodeChatView },
    { path: '/public/approvals/app', component: () => import('../src/views/PublicAppPreviewView.vue') },
  ],
})
const app = createApp(RouterView).directive('hover-ink', vHoverInk).use(i18n).use(router)
void router.isReady().then(() => app.mount('#app'))
