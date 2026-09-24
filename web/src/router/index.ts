import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { updateDocumentTitle } from '@/lib/shared/locale'
import { installAuthGuard } from '@/lib/shared/authGuard'
import { installRoutePendingGuards } from '@/lib/shared/routePending'
import LoginView from '@/views/LoginView.vue'

const routes: RouteRecordRaw[] = [
  // Eager: /login hosts brand LCP; avoid extra async chunk on critical path
  { path: '/login', name: 'login', component: LoginView, meta: { titleKey: 'route.login', public: true, bare: true } },
  {
    path: '/public/gate-approvals',
    name: 'public-gate-approval',
    component: () => import('@/views/PublicGateApprovalView.vue'),
    meta: { titleKey: 'route.publicGateApproval', public: true, bare: true },
  },
  {
    path: '/embed/runs/:runId/nodes/:nodeId/chat',
    name: 'embed-node-chat',
    component: () => import('@/views/EmbedNodeChatView.vue'),
    meta: { titleKey: 'route.embedChat', public: true, bare: true },
  },
  { path: '/', redirect: '/dashboard' },
  { path: '/dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue'), meta: { titleKey: 'route.dashboard' } },
  { path: '/stats', name: 'stats', component: () => import('@/views/TokenAnalyticsView.vue'), meta: { titleKey: 'route.stats' } },
  { path: '/board', name: 'board', component: () => import('@/views/BoardRedirectView.vue'), meta: { titleKey: 'route.board' } },
  { path: '/projects', name: 'projects', component: () => import('@/views/ProjectListView.vue'), meta: { titleKey: 'route.projects' } },
  { path: '/projects/:id', name: 'project-detail', component: () => import('@/views/ProjectDetailView.vue'), meta: { titleKey: 'route.projectDetail' } },
  { path: '/workflows', redirect: '/projects' },
  { path: '/workflows/:id/edit', name: 'workflow-editor', component: () => import('@/views/WorkflowEditorView.vue'), meta: { titleKey: 'route.workflowEditor', full: true } },
  { path: '/runs', name: 'runs', component: () => import('@/views/RunListView.vue'), meta: { titleKey: 'route.runs' } },
  { path: '/runs/:id', name: 'run-detail', component: () => import('@/views/RunDetailView.vue'), meta: { titleKey: 'route.runDetail', full: true } },
  { path: '/gates', name: 'gates', component: () => import('@/views/GatesInboxView.vue'), meta: { titleKey: 'route.gates' } },
  { path: '/artifacts', name: 'artifacts', component: () => import('@/views/ArtifactsView.vue'), meta: { titleKey: 'route.artifacts' } },
  { path: '/notifications', name: 'notifications', component: () => import('@/views/NotificationsView.vue'), meta: { titleKey: 'route.notifications' } },
  { path: '/agents', name: 'agents', component: () => import('@/views/AgentStudioView.vue'), meta: { titleKey: 'route.agents' } },
  { path: '/sandboxes', name: 'sandboxes', component: () => import('@/views/SandboxListView.vue'), meta: { titleKey: 'route.sandboxes' } },
  { path: '/sandboxes/:id/console', name: 'sandbox-console', component: () => import('@/views/SandboxConsoleView.vue'), meta: { titleKey: 'route.sandboxConsole', full: true } },
  // plan g1.2 / g1.3: retire standalone pages; redirect old bookmarks into settings
  { path: '/integrations', redirect: { path: '/settings', query: { integrations: '1' } } },
  { path: '/triggers', redirect: '/settings' },
  { path: '/settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { titleKey: 'route.settings' } },
  { path: '/settings/platform-rules', name: 'platform-rules', component: () => import('@/views/PlatformRulesView.vue'), meta: { titleKey: 'route.platformRules' } },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: () => ({ top: 0 }),
})

installRoutePendingGuards(router)
installAuthGuard(router)

router.afterEach((to) => {
  updateDocumentTitle(to.meta.titleKey as string | undefined)
})
