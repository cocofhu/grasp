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
  <div class="flex h-full min-h-0 flex-col" data-testid="projects-view">
    <SettingsPageHeader class="mb-5 shrink-0" icon="git" :title="t('pages.projects.title')" :description="t('pages.projects.subtitle')">
      <template #actions>
        <div v-if="projects.length > 4" class="relative">
          <Icon name="search" :size="14" class="pointer-events-none absolute left-2.5 top-2.5 text-txt3" />
          <input v-model="query" class="input w-56 !pl-8 text-[13px]" :placeholder="t('pages.projects.search')" />
        </div>
        <AppButton variant="primary" icon="plus" data-testid="projects-new" @click="openNew">{{ t('pages.projects.new') }}</AppButton>
      </template>
    </SettingsPageHeader>
    <SettingsStatStrip v-if="!error" class="mb-5 shrink-0" :items="overviewStats" :loading="loading" data-testid="projects-stats" />

    <p v-if="error" class="mb-3 border-l-2 border-err/40 py-2 pl-3 text-[13px] text-err">{{ error }}</p>

    <div class="scroll-area min-h-0 flex-1 overflow-y-auto">
      <div v-if="loading" class="py-10 text-center text-txt3"><Icon name="spinner" :size="16" class="inline" /></div>
      <EmptyState v-else-if="!projects.length" icon="git" :title="t('pages.projects.empty')" :desc="t('pages.projects.emptyHint')">
        <AppButton variant="primary" icon="plus" @click="openNew">{{ t('pages.projects.new') }}</AppButton>
      </EmptyState>
      <div v-else class="projects-grid">
        <article v-for="p in shown" :key="p.id" class="project-card" :data-testid="`project-card-${p.id}`">
          <div class="flex items-start gap-2">
            <Icon name="git" :size="16" class="mt-0.5 shrink-0 text-accent" />
            <div class="min-w-0 flex-1">
              <h3 class="truncate text-[14px] font-semibold text-txt">{{ p.name }}</h3>
              <p class="truncate font-mono text-[11.5px] text-txt3" :title="p.repoUrl">{{ repoShort(p.repoUrl) }}</p>
            </div>
            <button type="button" class="project-card__icon" :aria-label="t('common.buttons.edit')" :title="t('common.buttons.edit')" @click="openEdit(p)">
              <Icon name="edit" :size="14" />
            </button>
            <button type="button" class="project-card__icon project-card__icon--danger" :aria-label="t('common.buttons.delete')" :title="t('common.buttons.delete')" @click="askDelete(p)">
              <Icon name="trash" :size="14" />
            </button>
          </div>
          <p v-if="p.description" class="line-clamp-2 text-[12px] text-txt2">{{ p.description }}</p>
          <div class="flex flex-wrap gap-1.5">
            <span class="project-card__tag"><Icon name="branch" :size="11" />{{ p.defaultBranch || t('pages.solo.env.remoteDefault') }}</span>
          </div>
          <div class="mt-auto flex items-center justify-between gap-2 pt-1">
            <span class="text-[11px] text-txt3">{{ p.lastUsedAt ? t('pages.projects.lastUsed', { time: relTime(p.lastUsedAt) }) : t('pages.projects.neverUsed') }}</span>
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
.projects-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
  padding-bottom: 24px;
}
.project-card {
  display: flex;
  min-height: 150px;
  flex-direction: column;
  gap: 10px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  background: rgb(var(--c-surface));
  padding: 14px;
  transition: border-color 0.15s ease;
}
.project-card:hover {
  border-color: rgb(var(--c-accent) / 0.3);
}
.project-card__icon {
  display: flex;
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 7px;
  color: rgb(var(--c-txt3));
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
  padding: 1px 8px;
  color: rgb(var(--c-txt2));
  font-size: 11px;
}
.project-card__start {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border: 1px solid rgb(var(--c-accent) / 0.4);
  border-radius: 8px;
  padding: 3px 10px;
  color: rgb(var(--c-accent-2));
  font-size: 12px;
}
.project-card__start:hover {
  background: rgb(var(--c-accent) / 0.08);
}
</style>
