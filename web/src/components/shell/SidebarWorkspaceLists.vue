<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { api } from '@/lib/api/api'
import { displayRunTitle } from '@/lib/run/runTitle'
import type { Project, Run } from '@/lib/shared/types'

const props = defineProps<{ runTitles?: Record<string, string> }>()
const emit = defineEmits<{ (e: 'navigate'): void }>()
const { t } = useI18n()
const route = useRoute()
const projects = ref<Project[]>([])
const runs = ref<Run[]>([])
const loading = ref(false)
const projectsFailed = ref(false)
const runsFailed = ref(false)
const expandedProject = ref<string | null>(null)
type ProjectRunState = { status: 'loading' | 'ready' | 'error'; items: Run[] }
const projectRuns = ref<Record<string, ProjectRunState>>({})
const recentRuns = computed(() => runs.value.slice(0, 6))
const menuProject = ref<Project | null>(null)
const menuPanel = ref<HTMLElement | null>(null)
const menuTrigger = ref<HTMLButtonElement | null>(null)
const menuStyle = ref<Record<string, string>>({ visibility: 'hidden' })
const menuId = `workspace-project-menu-${useId().replace(/:/g, '')}`
let workspaceRequest: AbortController | null = null
let projectRequest: { id: string; controller: AbortController } | null = null

const projectPath = (id: string) => `/projects/${encodeURIComponent(id)}`
const runPath = (id: string) => `/runs/${encodeURIComponent(id)}`
const runTitle = (run: Run) => displayRunTitle(props.runTitles?.[run.id] ?? run.title) || run.workflowName || run.id
const queryId = (value: unknown) => Array.isArray(value) ? value[0] : value

function projectActive(id: string) {
  const path = projectPath(id)
  return route.path === path || route.path.startsWith(`${path}/`) ||
    (route.path === '/runs' && queryId(route.query.projectId) === id)
}

function runActive(id: string) {
  return route.path === runPath(id) ||
    ((route.path === '/gates' || route.path.startsWith('/gates/')) && queryId(route.query.run) === id)
}

function closeMenu(restoreFocus = false) {
  const trigger = menuTrigger.value
  menuProject.value = null
  menuTrigger.value = null
  if (restoreFocus && trigger?.isConnected) trigger.focus()
}

function placeMenu() {
  if (!menuProject.value || !menuPanel.value || !menuTrigger.value) return
  const anchor = menuTrigger.value.getBoundingClientRect()
  if (!menuTrigger.value.isConnected || anchor.bottom < 0 || anchor.top > window.innerHeight) {
    closeMenu()
    return
  }
  const box = menuPanel.value.getBoundingClientRect()
  const width = box.width || Math.min(210, window.innerWidth - 16)
  const height = box.height || 80
  const left = Math.max(8, Math.min(anchor.right - width, window.innerWidth - width - 8))
  const below = anchor.bottom + 6
  const top = below + height <= window.innerHeight - 8 ? below : Math.max(8, anchor.top - height - 6)
  menuStyle.value = { left: `${Math.round(left)}px`, top: `${Math.round(top)}px`, visibility: 'visible' }
}

async function toggleMenu(project: Project, event: MouseEvent) {
  if (menuProject.value?.id === project.id) {
    closeMenu(true)
    return
  }
  menuTrigger.value = event.currentTarget as HTMLButtonElement
  menuStyle.value = { visibility: 'hidden' }
  menuProject.value = project
  await nextTick()
  if (menuProject.value?.id !== project.id) return
  placeMenu()
  menuPanel.value?.querySelector<HTMLElement>('[role="menuitem"]')?.focus()
}

function menuNavigate() {
  closeMenu()
  emit('navigate')
}

function onMenuOutside(event: Event) {
  const target = event.target
  if (!(target instanceof Node) || menuPanel.value?.contains(target) || menuTrigger.value?.contains(target)) return
  closeMenu()
}

function onMenuKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    closeMenu(true)
    return
  }
  if (event.key === 'Tab') {
    // Restore the trigger before the browser's normal Tab action so the next
    // stop remains the row's compose action rather than the end of the portal.
    closeMenu(true)
    return
  }
  if (!menuPanel.value?.contains(document.activeElement)) return
  const items = Array.from(menuPanel.value.querySelectorAll<HTMLElement>('[role="menuitem"]'))
  const current = items.indexOf(document.activeElement as HTMLElement)
  let next: number
  if (event.key === 'ArrowDown') next = (current + 1) % items.length
  else if (event.key === 'ArrowUp') next = (current - 1 + items.length) % items.length
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = items.length - 1
  else return
  event.preventDefault()
  items[next]?.focus()
}

watch(menuProject, (project, _previous, onCleanup) => {
  if (!project) return
  document.addEventListener('pointerdown', onMenuOutside, true)
  document.addEventListener('focusin', onMenuOutside)
  document.addEventListener('keydown', onMenuKeydown)
  window.addEventListener('resize', placeMenu)
  window.addEventListener('scroll', placeMenu, true)
  onCleanup(() => {
    document.removeEventListener('pointerdown', onMenuOutside, true)
    document.removeEventListener('focusin', onMenuOutside)
    document.removeEventListener('keydown', onMenuKeydown)
    window.removeEventListener('resize', placeMenu)
    window.removeEventListener('scroll', placeMenu, true)
  })
})

function cancelProjectRequest() {
  const pending = projectRequest
  if (!pending) return
  projectRequest = null
  pending.controller.abort()
  if (projectRuns.value[pending.id]?.status === 'loading') delete projectRuns.value[pending.id]
}

async function loadProjectRuns(id: string) {
  cancelProjectRequest()
  const request = { id, controller: new AbortController() }
  projectRequest = request
  projectRuns.value[id] = { status: 'loading', items: [] }
  try {
    const result = await api.listRuns({
      projectId: id, page: 1, pageSize: 2, sort: 'started_at', order: 'desc',
      signal: request.controller.signal,
    })
    if (projectRequest !== request) return
    if (!Array.isArray(result?.items)) throw new Error('Invalid run list')
    projectRuns.value[id] = { status: 'ready', items: result.items.slice(0, 2) }
  } catch {
    if (projectRequest !== request) return
    projectRuns.value[id] = { status: 'error', items: [] }
  } finally {
    if (projectRequest === request) projectRequest = null
  }
}

function toggleProject(id: string) {
  cancelProjectRequest()
  expandedProject.value = expandedProject.value === id ? null : id
  if (expandedProject.value && projectRuns.value[id]?.status !== 'ready') void loadProjectRuns(id)
}

async function loadWorkspace() {
  workspaceRequest?.abort()
  const request = new AbortController()
  workspaceRequest = request
  loading.value = true
  const expanded = expandedProject.value
  const previousItems = expanded ? projectRuns.value[expanded]?.items ?? [] : []
  cancelProjectRequest()
  // Collapsed results are cached within the current route. Keep the open tree
  // in a loading state until its fresh query finishes, including an empty tree.
  projectRuns.value = expanded ? { [expanded]: { status: 'loading', items: previousItems } } : {}
  const [projectResult, runResult] = await Promise.allSettled([
    api.listProjects({ signal: request.signal }),
    api.listRuns({ page: 1, pageSize: 20, sort: 'started_at', order: 'desc', signal: request.signal }),
  ])
  if (workspaceRequest !== request) return
  projectsFailed.value = projectResult.status !== 'fulfilled' || !Array.isArray(projectResult.value)
  projects.value = projectResult.status === 'fulfilled' && Array.isArray(projectResult.value)
    ? projectResult.value.slice(0, 6) : []
  runsFailed.value = runResult.status !== 'fulfilled' || !Array.isArray(runResult.value?.items)
  runs.value = runResult.status === 'fulfilled' && Array.isArray(runResult.value?.items)
    ? runResult.value.items : []
  if (expandedProject.value && !projects.value.some((project) => project.id === expandedProject.value)) {
    expandedProject.value = null
    cancelProjectRequest()
  } else if (expandedProject.value && projectRequest?.id !== expandedProject.value) {
    await loadProjectRuns(expandedProject.value)
  }
  if (workspaceRequest !== request) return
  loading.value = false
  workspaceRequest = null
}

watch(() => route.fullPath, () => {
  closeMenu()
  void loadWorkspace()
}, { immediate: true })
onBeforeUnmount(() => {
  workspaceRequest?.abort()
  workspaceRequest = null
  cancelProjectRequest()
})
</script>

<template>
  <div class="workspace-lists" data-testid="sidebar-workspace-lists" :aria-busy="loading">
    <p v-if="loading && !projects.length && !runs.length" class="workspace-list-state" role="status">
      {{ t('nav.workspaceLoading') }}
    </p>

    <section data-testid="sidebar-projects">
      <h3 class="workspace-list-heading">
        <RouterLink to="/projects" :aria-label="t('nav.viewAllProjects')" @click="emit('navigate')">
          <span>{{ t('nav.projects') }}</span>
        </RouterLink>
      </h3>
      <div v-if="projectsFailed" class="workspace-list-state" role="status">
        <span>{{ t('nav.workspaceLoadFailed') }}</span>
        <button type="button" class="workspace-list-retry" @click="loadWorkspace">{{ t('nav.workspaceRetry') }}</button>
      </div>
      <p v-else-if="!loading && !projects.length" class="workspace-list-state">{{ t('nav.workspaceNoProjects') }}</p>
      <div v-for="project in projects" :key="project.id" data-testid="sidebar-project">
        <div
          class="workspace-project-row"
          :class="{
            'workspace-list-row--active': projectActive(project.id),
            'workspace-project-row--menu-open': menuProject?.id === project.id,
          }"
        >
          <button
            type="button"
            class="workspace-project-toggle"
            data-testid="sidebar-project-toggle"
            :aria-expanded="expandedProject === project.id"
            :aria-label="t(expandedProject === project.id ? 'nav.collapseProject' : 'nav.expandProject', { name: project.name })"
            @click="toggleProject(project.id)"
          >
            <Icon name="folder" :size="16" class="workspace-project-folder" aria-hidden="true" />
            <Icon name="chevron-right" :size="13" class="workspace-project-chevron" :class="{ 'workspace-project-chevron--open': expandedProject === project.id }" aria-hidden="true" />
          </button>
          <RouterLink
            :to="projectPath(project.id)"
            class="workspace-project-link"
            data-testid="sidebar-project-link"
            :aria-current="projectActive(project.id) ? 'page' : undefined"
            :title="project.name"
            @click="emit('navigate')"
          >
            <span class="workspace-list-title">{{ project.name }}</span>
          </RouterLink>
          <div class="workspace-project-actions">
            <button
              type="button"
              class="workspace-project-action"
              data-testid="sidebar-project-menu-trigger"
              aria-haspopup="menu"
              :aria-expanded="menuProject?.id === project.id"
              :aria-controls="menuProject?.id === project.id ? menuId : undefined"
              :aria-label="t('nav.projectActions', { name: project.name })"
              :title="t('nav.projectActions', { name: project.name })"
              @click="toggleMenu(project, $event)"
            ><Icon name="more-horizontal" :size="16" aria-hidden="true" /></button>
            <RouterLink
              :to="{ path: '/dashboard', query: { projectId: project.id } }"
              class="workspace-project-action"
              data-testid="sidebar-project-compose"
              :aria-label="t('nav.newProjectRequest', { name: project.name })"
              :title="t('nav.newProjectRequest', { name: project.name })"
              @click="emit('navigate')"
            ><Icon name="new-request" :size="15" aria-hidden="true" /></RouterLink>
          </div>
        </div>
        <div v-if="expandedProject === project.id" class="workspace-project-children">
          <p v-if="projectRuns[project.id]?.status === 'loading'" class="workspace-list-state" role="status">{{ t('nav.workspaceLoading') }}</p>
          <div v-else-if="projectRuns[project.id]?.status === 'error'" class="workspace-list-state" role="status">
            <span>{{ t('nav.workspaceLoadFailed') }}</span>
            <button type="button" class="workspace-list-retry" @click="loadProjectRuns(project.id)">{{ t('nav.workspaceRetry') }}</button>
          </div>
          <template v-else>
            <RouterLink
              v-for="run in projectRuns[project.id]?.items"
              :key="run.id"
              :to="runPath(run.id)"
              class="workspace-list-row"
              :class="{ 'workspace-list-row--active': runActive(run.id) }"
              :aria-current="runActive(run.id) ? 'page' : undefined"
              :title="runTitle(run)"
              data-testid="sidebar-project-run"
              @click="emit('navigate')"
            >
              <Icon name="chat" :size="15" aria-hidden="true" />
              <span class="workspace-list-title">{{ runTitle(run) }}</span>
            </RouterLink>
            <p v-if="!projectRuns[project.id]?.items.length" class="workspace-list-state">{{ t('nav.workspaceNoRuns') }}</p>
          </template>
        </div>
      </div>
    </section>

    <section data-testid="sidebar-recent">
      <h3 class="workspace-list-heading">
        <RouterLink to="/runs" :aria-label="t('nav.viewAllRuns')" @click="emit('navigate')">
          <span>{{ t('nav.recent') }}</span>
        </RouterLink>
      </h3>
      <div v-if="runsFailed" class="workspace-list-state" role="status">
        <span>{{ t('nav.workspaceLoadFailed') }}</span>
        <button type="button" class="workspace-list-retry" @click="loadWorkspace">{{ t('nav.workspaceRetry') }}</button>
      </div>
      <p v-else-if="!loading && !recentRuns.length" class="workspace-list-state">{{ t('nav.workspaceNoRuns') }}</p>
      <RouterLink
        v-for="run in recentRuns"
        :key="run.id"
        :to="runPath(run.id)"
        class="workspace-list-row"
        :class="{ 'workspace-list-row--active': runActive(run.id) }"
        :aria-current="runActive(run.id) ? 'page' : undefined"
        :title="runTitle(run)"
        data-testid="sidebar-recent-run"
        @click="emit('navigate')"
      >
        <Icon name="chat" :size="15" aria-hidden="true" />
        <span class="workspace-list-title">{{ runTitle(run) }}</span>
      </RouterLink>
    </section>
    <Teleport to="body">
      <div
        v-if="menuProject"
        :id="menuId"
        ref="menuPanel"
        class="workspace-project-menu"
        role="menu"
        :aria-label="t('nav.projectActions', { name: menuProject.name })"
        :style="menuStyle"
        data-testid="sidebar-project-menu"
      >
        <RouterLink
          :to="projectPath(menuProject.id)"
          role="menuitem"
          tabindex="-1"
          data-testid="sidebar-project-menu-open"
          @click="menuNavigate"
        ><Icon name="folder" :size="15" aria-hidden="true" /><span>{{ t('nav.viewProject') }}</span></RouterLink>
        <RouterLink
          :to="{ path: '/runs', query: { projectId: menuProject.id } }"
          role="menuitem"
          tabindex="-1"
          data-testid="sidebar-project-menu-runs"
          @click="menuNavigate"
        ><Icon name="runs" :size="15" aria-hidden="true" /><span>{{ t('nav.viewProjectRuns') }}</span></RouterLink>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.workspace-lists { min-width: 0; font-size: 13px; }
.workspace-lists section + section { margin-top: 24px; }
.workspace-list-heading { margin: 0 0 4px; font-size: 11.5px; font-weight: 600; color: color-mix(in srgb, rgb(var(--c-txt2)) 70%, rgb(var(--c-txt3))); }
.workspace-list-heading a { display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; height: 26px; padding: 0 10px; border-radius: 6px; }
.workspace-list-heading a:hover { color: rgb(var(--c-txt2)); }
.workspace-list-row {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  min-width: 0;
  padding: 0 10px;
  border-radius: 9px;
  color: rgb(var(--c-txt2));
  font-size: 13px;
  transition: background-color 160ms ease, color 160ms ease;
}
.workspace-list-row:hover { background: rgb(var(--c-txt) / .035); color: rgb(var(--c-txt)); }
.workspace-list-row--active, .workspace-list-row--active:hover { color: rgb(var(--c-txt)); background: color-mix(in srgb, rgb(var(--c-accent-2) / .07), rgb(var(--c-txt) / .04)); }
.workspace-list-row--active::before { content: ''; position: absolute; left: 0; top: 11px; bottom: 11px; width: 2px; border-radius: 2px; background: rgb(var(--c-accent-2) / .45); }
.workspace-list-row > svg { flex: none; color: rgb(var(--c-txt2) / .72); }
.workspace-list-title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.workspace-project-row { position: relative; display: flex; align-items: center; gap: 2px; min-width: 0; height: 36px; padding: 0 4px; border-radius: 9px; color: rgb(var(--c-txt2)); }
.workspace-project-row:hover { background: rgb(var(--c-txt) / .035); color: rgb(var(--c-txt)); }
.workspace-project-row.workspace-list-row--active { background: color-mix(in srgb, rgb(var(--c-accent-2) / .07), rgb(var(--c-txt) / .04)); }
.workspace-project-link { display: flex; align-items: center; flex: 1; height: 32px; min-width: 0; border-radius: 5px; }
.workspace-project-toggle { position: relative; display: grid; place-items: center; width: 28px; height: 30px; flex: none; border-radius: 6px; color: rgb(var(--c-txt2) / .72); }
.workspace-project-toggle:hover { background: rgb(var(--c-txt) / .04); color: rgb(var(--c-txt)); }
.workspace-project-folder, .workspace-project-chevron { grid-area: 1 / 1; transition: opacity 160ms ease, transform 160ms ease; }
.workspace-project-chevron { opacity: 0; }
.workspace-project-row:is(:hover, :focus-within) .workspace-project-folder { opacity: 0; }
.workspace-project-row:is(:hover, :focus-within) .workspace-project-chevron { opacity: 1; }
.workspace-project-chevron--open { transform: rotate(90deg); }
.workspace-project-actions { display: flex; align-items: center; gap: 2px; flex: none; opacity: 0; pointer-events: none; transition: opacity 160ms ease; }
.workspace-project-row:is(:hover, :focus-within, .workspace-project-row--menu-open) .workspace-project-actions { opacity: 1; pointer-events: auto; }
.workspace-project-action { display: grid; place-items: center; width: 24px; height: 28px; border-radius: 6px; color: rgb(var(--c-txt2) / .8); }
.workspace-project-action:hover { color: rgb(var(--c-txt)); background: rgb(var(--c-txt) / .055); }
.workspace-project-children { margin: 2px 0 4px 18px; padding-left: 7px; border-left: 1px solid rgb(var(--c-line) / .65); }
.workspace-list-state { display: flex; align-items: center; gap: 6px; margin: 0; padding: 9px 10px; font-size: 12px; line-height: 18px; color: rgb(var(--c-txt3)); }
.workspace-list-retry { flex-shrink: 0; color: rgb(var(--c-txt2)); text-decoration: underline; text-underline-offset: 3px; }
.workspace-lists a:focus-visible, .workspace-lists button:focus-visible { outline: 2px solid rgb(var(--c-accent-2) / .5); outline-offset: 1px; }
.workspace-project-menu { position: fixed; top: 0; left: 0; z-index: 80; width: 210px; max-width: calc(100vw - 16px); max-height: calc(100dvh - 16px); overflow-y: auto; padding: 4px; border: 1px solid rgb(var(--c-line)); border-radius: 10px; background: rgb(var(--c-surface)); box-shadow: 0 8px 24px rgb(0 0 0 / .12), 0 2px 5px rgb(0 0 0 / .04); }
.workspace-project-menu a { display: flex; align-items: center; gap: 8px; min-height: 34px; padding: 6px 9px; border-radius: 6px; color: rgb(var(--c-txt2)); font-size: 13px; }
.workspace-project-menu a:is(:hover, :focus) { color: rgb(var(--c-txt)); background: rgb(var(--c-txt) / .05); }
.workspace-project-menu a:focus-visible { outline: 2px solid rgb(var(--c-accent-2) / .4); outline-offset: -2px; }
.workspace-project-menu svg { flex: none; color: rgb(var(--c-txt3)); }
@media (hover: none), (pointer: coarse) {
  .workspace-project-actions { opacity: 1; pointer-events: auto; }
}
@media (prefers-reduced-motion: reduce) {
  .workspace-list-row, .workspace-project-toggle > svg, .workspace-project-actions { transition: none; }
}
</style>
