<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import SettingsPageHeader from '@/components/settings/SettingsPageHeader.vue'
import SettingsStatStrip from '@/components/settings/SettingsStatStrip.vue'
import type { SettingsStatItem } from '@/components/settings/settingsStats'
import ProjectDrawer from '@/components/solo/ProjectDrawer.vue'
import { api } from '@/lib/api/api'
import type { Project } from '@/lib/api/apiTypes'
import { relTime } from '@/lib/shared/format'
import { repoShort } from '@/lib/solo/soloPrefs'
import { soloErrorText } from '@/lib/solo/soloErrors'

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()

const projects = ref<Project[]>([])
const loading = ref(true)
const error = ref('')
const query = ref('')
const drawerOpen = ref(false)
const editing = ref<Project | null>(null)
const deleting = ref<Project | null>(null)
const deleteActive = ref(0)
const deleteBusy = ref(false)
const deleteError = ref('')

const overviewStats = computed<SettingsStatItem[]>(() => {
  const hosts = new Set(projects.value.map((p) => repoHost(p.repoUrl)).filter(Boolean))
  return [
    { key: 'total', icon: 'git', label: t('pages.settingsOverview.stats.projectsTotal'), value: projects.value.length, tone: 'accent' },
    { key: 'used', icon: 'clock', label: t('pages.settingsOverview.stats.projectsUsed'), value: projects.value.filter((p) => !!p.lastUsedAt).length, total: projects.value.length },
    { key: 'hosts', icon: 'globe', label: t('pages.settingsOverview.stats.projectsHosts'), value: hosts.size },
  ]
})

function repoHost(url: string): string {
  const raw = (url || '').trim()
  const scp = /^[^@/\s]+@([^:/\s]+):/.exec(raw)
  if (scp) return scp[1]!.toLowerCase()
  try { return new URL(raw).hostname.toLowerCase() } catch { return '' }
}

const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return projects.value
  return projects.value.filter((p) => p.name.toLowerCase().includes(q) || p.repoUrl.toLowerCase().includes(q))
})

async function load() {
  loading.value = true
  try {
    projects.value = await api.listProjects()
    error.value = ''
  } catch (e) {
    error.value = soloErrorText(e, t, te)
  } finally {
    loading.value = false
  }
}

function openNew() {
  editing.value = null
  drawerOpen.value = true
}

function openEdit(p: Project) {
  editing.value = p
  drawerOpen.value = true
}

function onSaved(p: Project) {
  drawerOpen.value = false
  const i = projects.value.findIndex((x) => x.id === p.id)
  if (i >= 0) projects.value.splice(i, 1, p)
  else projects.value.unshift(p)
}

async function askDelete(p: Project) {
  deleting.value = p
  deleteActive.value = 0
  deleteError.value = ''
  try {
    deleteActive.value = (await api.getProject(p.id)).activeSessions
  } catch {
    /* count is advisory */
  }
}

async function confirmDelete() {
  const p = deleting.value
  if (!p) return
  deleteBusy.value = true
  try {
    await api.deleteProject(p.id)
    projects.value = projects.value.filter((x) => x.id !== p.id)
    deleting.value = null
  } catch (e) {
    deleteError.value = soloErrorText(e, t, te)
  } finally {
    deleteBusy.value = false
  }
}

// ?edit=<id> deep link (from a Solo session's "edit project").
watch(
  () => [route.query.edit, loading.value] as const,
  ([id, busy]) => {
    if (busy || typeof id !== 'string') return
    const hit = projects.value.find((p) => p.id === id)
    if (hit) openEdit(hit)
    void router.replace({ path: route.path })
  },
)

// ?new=1 deep link (from the Agent repository access picker).
watch(
  () => route.query.new,
  (flag) => {
    if (flag !== '1') return
    openNew()
    void router.replace({ path: route.path })
  },
  { immediate: true },
)

onMounted(load)
</script>

<template>
  <div class="projects-page flex h-full min-h-0 flex-col" data-testid="projects-view">
    <SettingsPageHeader class="projects-page__header mb-5 shrink-0" icon="git" :title="t('pages.projects.title')" :description="t('pages.projects.subtitle')">
      <template #actions>
        <label v-if="projects.length" class="projects-search">
          <Icon name="search" :size="14" class="pointer-events-none absolute left-2.5 top-2.5 text-txt3" />
          <input v-model="query" class="input w-64 !pl-8 text-[13px]" :placeholder="t('pages.projects.search')" />
        </label>
        <AppButton variant="primary" icon="plus" data-testid="projects-new" @click="openNew">{{ t('pages.projects.new') }}</AppButton>
      </template>
    </SettingsPageHeader>
    <SettingsStatStrip v-if="!error" class="mb-5 shrink-0" :items="overviewStats" :loading="loading" data-testid="projects-stats" />

    <p v-if="error" class="mb-3 border-l-2 border-err/40 py-2 pl-3 text-[13px] text-err">{{ error }}</p>

    <div class="projects-content scroll-area min-h-0 flex-1 overflow-y-auto">
      <div v-if="loading" class="py-10 text-center text-txt3"><Icon name="spinner" :size="16" class="inline" /></div>
      <EmptyState v-else-if="!projects.length" icon="git" :title="t('pages.projects.empty')" :desc="t('pages.projects.emptyHint')">
        <AppButton variant="primary" icon="plus" @click="openNew">{{ t('pages.projects.new') }}</AppButton>
      </EmptyState>
      <div v-else-if="!shown.length" class="projects-no-results">
        <span class="projects-no-results__icon" aria-hidden="true"><Icon name="search" :size="18" :interactive="false" /></span>
        <p class="projects-no-results__title">{{ t('pages.projects.noMatch') }}</p>
        <p class="projects-no-results__hint">{{ t('pages.projects.noMatchHint') }}</p>
      </div>
      <div v-else class="projects-grid">
        <article v-for="p in shown" :key="p.id" class="project-card" :data-testid="`project-card-${p.id}`">
          <div class="project-card__header">
            <div class="project-card__identity">
              <span class="project-card__mark" aria-hidden="true"><Icon name="git" :size="17" :interactive="false" /></span>
              <div class="min-w-0">
                <h3 class="truncate text-[15px] font-semibold text-txt">{{ p.name }}</h3>
                <p class="project-card__repo truncate font-mono" :title="p.repoUrl">{{ repoShort(p.repoUrl) }}</p>
              </div>
            </div>
            <div class="project-card__actions">
              <button type="button" class="project-card__icon" :aria-label="t('common.buttons.edit')" :title="t('common.buttons.edit')" @click="openEdit(p)">
                <Icon name="edit" :size="14" />
              </button>
              <button type="button" class="project-card__icon project-card__icon--danger" :aria-label="t('common.buttons.delete')" :title="t('common.buttons.delete')" @click="askDelete(p)">
                <Icon name="trash" :size="14" />
              </button>
            </div>
          </div>
          <div class="project-card__body">
            <p v-if="p.description" class="project-card__description line-clamp-2 text-[12px] text-txt2">{{ p.description }}</p>
            <div class="project-card__meta">
              <span class="project-card__tag"><Icon name="branch" :size="11" :interactive="false" />{{ p.defaultBranch || t('pages.solo.env.remoteDefault') }}</span>
              <span v-if="repoHost(p.repoUrl)" class="project-card__host"><span class="project-card__host-dot" aria-hidden="true" />{{ repoHost(p.repoUrl) }}</span>
            </div>
          </div>
          <div class="project-card__footer">
            <span class="project-card__activity" :class="{ 'project-card__activity--used': p.lastUsedAt }">
              <Icon name="clock" :size="12" :interactive="false" />
              {{ p.lastUsedAt ? t('pages.projects.lastUsed', { time: relTime(p.lastUsedAt) }) : t('pages.projects.neverUsed') }}
            </span>
            <RouterLink :to="`/solo?new=1&project=${encodeURIComponent(p.id)}`" class="project-card__start" :data-testid="`project-start-${p.id}`">
              <Icon name="terminal" :size="13" />{{ t('pages.projects.start') }}
            </RouterLink>
          </div>
        </article>
      </div>
    </div>

    <ProjectDrawer :open="drawerOpen" :project="editing" @close="drawerOpen = false" @saved="onSaved" />

    <AppModal :open="!!deleting" :title="t('pages.projects.delete.title')" :width="420" close-on-esc @close="deleting = null">
      <div class="space-y-2 text-[13px] text-txt2">
        <p>{{ t('pages.projects.delete.body', { name: deleting?.name || '' }) }}</p>
        <p v-if="deleteActive" class="text-warn">{{ t('pages.projects.delete.active', { n: deleteActive }) }}</p>
        <p v-if="deleteError" class="text-err">{{ deleteError }}</p>
      </div>
      <template #footer>
        <AppButton variant="ghost" @click="deleting = null">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="danger" icon="trash" :loading="deleteBusy" @click="confirmDelete">{{ t('common.buttons.delete') }}</AppButton>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
.projects-page {
  width: 100%;
  max-width: 1680px;
  margin-inline: auto;
}
.projects-page__header {
  padding-bottom: 18px;
  border-bottom: 1px solid rgb(var(--c-line) / .6);
}
.projects-search {
  position: relative;
  display: block;
}
.projects-content {
  padding-top: 20px;
}
.projects-no-results {
  display: flex;
  min-height: 220px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  border: 1px dashed rgb(var(--c-line-strong) / .7);
  border-radius: 16px;
  background: rgb(var(--c-surface) / .55);
  text-align: center;
}
.projects-no-results__icon {
  display: flex;
  width: 42px;
  height: 42px;
  align-items: center;
  justify-content: center;
  border-radius: 12px;
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt2));
}
.projects-no-results__title {
  margin: 14px 0 0;
  color: rgb(var(--c-txt));
  font-size: 14px;
  font-weight: 600;
}
.projects-no-results__hint {
  margin: 4px 0 0;
  color: rgb(var(--c-txt3));
  font-size: 12px;
}
.projects-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 270px), 1fr));
  gap: 14px;
  padding-bottom: 24px;
}
.project-card {
  position: relative;
  overflow: hidden;
  display: flex;
  min-height: 224px;
  flex-direction: column;
  border: 1px solid rgb(var(--c-line));
  border-radius: 13px;
  background: rgb(var(--c-surface) / .78);
  transition: border-color var(--dur-ui), box-shadow var(--dur-ui), transform var(--dur-ui);
}
.project-card:hover {
  border-color: rgb(var(--c-accent) / .38);
  background: rgb(var(--c-surface));
  box-shadow: 0 8px 18px rgb(var(--c-txt) / .06);
  transform: translateY(-1px);
}
.project-card__header,
.project-card__identity,
.project-card__actions,
.project-card__meta,
.project-card__footer {
  display: flex;
  align-items: center;
}
.project-card__header {
  justify-content: space-between;
  gap: 12px;
  padding: 16px 16px 13px;
}
.project-card__identity {
  min-width: 0;
  gap: 10px;
}
.project-card__mark {
  display: flex;
  width: 34px;
  height: 34px;
  flex: none;
  align-items: center;
  justify-content: center;
  border: 1px solid rgb(var(--c-accent) / .22);
  border-radius: 9px;
  background: rgb(var(--c-accent) / .08);
  color: rgb(var(--c-accent-2));
}
.project-card__repo {
  margin-top: 2px;
  color: rgb(var(--c-txt3));
  font-size: 10.5px;
  line-height: 16px;
}
.project-card__actions {
  flex: none;
  gap: 1px;
}
.project-card__body {
  display: flex;
  min-height: 84px;
  flex: 1;
  flex-direction: column;
  justify-content: space-between;
  gap: 14px;
  padding: 14px 16px 12px;
}
.project-card__description {
  min-height: 36px;
  line-height: 18px;
}
.project-card__meta {
  flex-wrap: wrap;
  gap: 8px;
}
.project-card__host {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: rgb(var(--c-txt3));
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
}
.project-card__host-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: rgb(var(--c-ok));
  box-shadow: 0 0 0 3px rgb(var(--c-ok) / .12);
}
.project-card__footer {
  justify-content: space-between;
  gap: 12px;
  border-top: 1px solid rgb(var(--c-line) / .72);
  padding: 12px 16px 14px;
  background: rgb(var(--c-base) / .12);
}
.project-card__activity {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  color: rgb(var(--c-txt3));
  font-size: 11.5px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.project-card__activity--used { color: rgb(var(--c-txt2)); }
.project-card__activity--used .app-icon { color: rgb(var(--c-accent-2)); }
.project-card__icon {
  display: flex;
  width: 26px;
  height: 26px;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  color: rgb(var(--c-txt3));
  transition: background var(--dur-ui), color var(--dur-ui);
}
.project-card__icon:hover {
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt));
}
.project-card__icon--danger:hover {
  color: rgb(var(--c-err));
}
.project-card__tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 999px;
  background: rgb(var(--c-elevated) / .58);
  padding: 3px 9px;
  color: rgb(var(--c-txt2));
  font-size: 11px;
}
.project-card__start {
  display: inline-flex;
  min-height: 32px;
  align-items: center;
  gap: 5px;
  border: 1px solid rgb(var(--c-accent) / .38);
  border-radius: 8px;
  background: transparent;
  padding: 0 11px;
  color: rgb(var(--c-accent-2));
  font-size: 11.5px;
  font-weight: 500;
  box-shadow: none;
  transition: background var(--dur-ui), border-color var(--dur-ui), box-shadow var(--dur-ui), transform var(--dur-ui);
}
.project-card__start:hover {
  border-color: rgb(var(--c-accent-hover));
  background: rgb(var(--c-accent-dim));
  box-shadow: none;
  transform: none;
}
@media (max-width: 767px) {
  .projects-page__header { padding-bottom: 16px; }
  .projects-search { flex: 1 1 180px; }
  .projects-search input { width: 100%; }
  .projects-content { padding-top: 16px; }
  .project-card { min-height: 208px; }
}
</style>
