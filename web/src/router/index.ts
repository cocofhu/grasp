import { createRouter, createWebHistory, type RouteRecordRaw, type RouterHistory } from 'vue-router'
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
  {
    path: '/embed/runs/:runId/nodes/:nodeId/artifacts',
    name: 'embed-node-artifacts',
    component: () => import('@/views/EmbedNodeArtifactsView.vue'),
    meta: { titleKey: 'route.embedArtifacts', public: true, bare: true },
  },
  {
    path: '/embed/runs/:runId/nodes/:nodeId/image',
    name: 'embed-node-image',
    component: () => import('@/views/EmbedNodeImageView.vue'),
    meta: { titleKey: 'route.embedImage', public: true, bare: true },
  },
  {
    path: '/embed/runs/:runId/nodes/:nodeId/demo',
    name: 'embed-node-demo',
    component: () => import('@/views/EmbedNodeDemoView.vue'),
    meta: { titleKey: 'route.embedDemo', public: true, bare: true },
  },
  { path: '/', redirect: '/dashboard' },
  { path: '/dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue'), meta: { titleKey: 'route.dashboard' } },
  { path: '/stats', name: 'stats', component: () => import('@/views/TokenAnalyticsView.vue'), meta: { titleKey: 'route.stats' } },
  { path: '/workflows', name: 'workflows', component: () => import('@/views/WorkflowsView.vue'), meta: { titleKey: 'route.workflows' } },
  { path: '/board', name: 'board', component: () => import('@/views/WorkspaceToolView.vue'), meta: { titleKey: 'route.board', workspaceTool: 'board', workspaceTitle: 'nav.board' } },
  { path: '/requirements', name: 'requirements', component: () => import('@/views/WorkspaceToolView.vue'), meta: { titleKey: 'route.requirements', workspaceTool: 'requirements', workspaceTitle: 'nav.requirements' } },
  { path: '/assistant', name: 'assistant', component: () => import('@/views/WorkspaceToolView.vue'), meta: { titleKey: 'route.assistant', workspaceTool: 'assistant', workspaceTitle: 'nav.assistant' } },
  { path: '/settings/assistant', name: 'assistantSettings', component: () => import('@/views/WorkspaceToolView.vue'), meta: { titleKey: 'route.assistantSettings', workspaceTool: 'assistantSettings', workspaceTitle: 'nav.assistantSettings' } },
  { path: '/settings/schedules', name: 'schedules', component: () => import('@/views/WorkspaceToolView.vue'), meta: { titleKey: 'route.schedules', workspaceTool: 'schedules', workspaceTitle: 'nav.schedules' } },
  { path: '/settings/notification-policy', name: 'notificationPolicy', component: () => import('@/views/WorkspaceToolView.vue'), meta: { titleKey: 'route.notificationPolicy', workspaceTool: 'notificationPolicy', workspaceTitle: 'nav.notificationPolicy' } },
  { path: '/settings/external-mcp', name: 'externalMcp', component: () => import('@/views/WorkspaceToolView.vue'), meta: { titleKey: 'route.externalMcp', workspaceTool: 'externalMcp', workspaceTitle: 'nav.externalMcp' } },
  { path: '/settings/shared-agent', name: 'sharedAgent', component: () => import('@/views/WorkspaceToolView.vue'), meta: { titleKey: 'route.sharedAgent', workspaceTool: 'sharedAgent', workspaceTitle: 'nav.sharedAgent' } },
  { path: '/settings/credentials', name: 'credentials', component: () => import('@/views/WorkspaceToolView.vue'), meta: { titleKey: 'route.credentials', workspaceTool: 'credentials', workspaceTitle: 'nav.credentials' } },
  { path: '/settings/audit', name: 'audit', component: () => import('@/views/WorkspaceToolView.vue'), meta: { titleKey: 'route.audit', workspaceTool: 'audit', workspaceTitle: 'nav.audit' } },
  { path: '/settings/variables', name: 'variables', component: () => import('@/views/WorkspaceDefaultsView.vue'), meta: { titleKey: 'route.variables', workspaceTool: 'variables', workspaceTitle: 'nav.variables' } },
  { path: '/settings/workspace', name: 'workspaceInfo', component: () => import('@/views/WorkspaceDefaultsView.vue'), meta: { titleKey: 'route.workspaceInfo', workspaceTool: 'workspaceInfo', workspaceTitle: 'nav.workspaceInfo' } },
  { path: '/workflows/:id/edit', name: 'workflow-editor', component: () => import('@/views/WorkflowEditorView.vue'), meta: { titleKey: 'route.workflowEditor', full: true } },
  { path: '/runs', name: 'runs', component: () => import('@/views/RunListView.vue'), meta: { titleKey: 'route.runs' } },
  { path: '/runs/:id', name: 'run-detail', component: () => import('@/views/RunDetailView.vue'), meta: { titleKey: 'route.runDetail', full: true } },
  { path: '/gates', name: 'gates', component: () => import('@/views/GatesInboxView.vue'), meta: { titleKey: 'route.gates' } },
  { path: '/artifacts', name: 'artifacts', component: () => import('@/views/ArtifactsView.vue'), meta: { titleKey: 'route.artifacts' } },
  { path: '/notifications', name: 'notifications', component: () => import('@/views/NotificationsView.vue'), meta: { titleKey: 'route.notifications' } },
  { path: '/agents', name: 'agents', component: () => import('@/views/AgentStudioView.vue'), meta: { titleKey: 'route.agents' } },
  { path: '/sandboxes', name: 'sandboxes', component: () => import('@/views/SandboxListView.vue'), meta: { titleKey: 'route.sandboxes' } },
  { path: '/sandboxes/:id/console', name: 'sandbox-console', component: () => import('@/views/SandboxConsoleView.vue'), meta: { titleKey: 'route.sandboxConsole', full: true } },
  { path: '/settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { titleKey: 'route.settings' } },
  // Bookmarks from the multi-project UI.
  { path: '/projects/:rest(.*)*', redirect: '/workflows' },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
]

/** Shared production route table for the app and browser acceptance harnesses. */
export function createAppRouter(history: RouterHistory = createWebHistory()) {
  const instance = createRouter({ history, routes, scrollBehavior: () => ({ top: 0 }) })
  installRoutePendingGuards(instance)
  installAuthGuard(instance)
  instance.afterEach((to) => updateDocumentTitle(to.meta.titleKey as string | undefined))
  return instance
}

export const router = createAppRouter()
