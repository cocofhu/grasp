<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Router } from 'vue-router'
import { api, type Project } from '@/lib/api/api'
import { repoProtocol, repoShort } from '@/lib/solo/soloPrefs'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import Icon from '@/components/ui/Icon.vue'

/**
 * Multi-select of registered repositories whose own Git credentials are
 * injected per repository. With `inherited` set, the value may be undefined,
 * meaning "inherit the workspace default".
 */
const props = defineProps<{
  modelValue?: string[]
  /** The workspace default selection; enables the inherit switch. */
  inherited?: string[]
  /** Copy variant for the workspace default screen. */
  shared?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: string[] | undefined] }>()

const { t } = useI18n()

// Optional: the picker also renders in panels mounted without a router.
const router = getCurrentInstance()?.appContext.config.globalProperties.$router as Router | undefined
function go(path: string) {
  if (router) void router.push(path)
  else window.location.assign(path)
}

const repos = ref<Project[]>([])
const loading = ref(true)
const loadError = ref(false)
const query = ref('')

async function load() {
  loading.value = true
  loadError.value = false
  try {
    repos.value = await api.listProjects()
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
}
onMounted(load)

const inheritable = computed(() => props.inherited !== undefined)
const inheriting = computed(() => inheritable.value && props.modelValue === undefined)
const selected = computed(() => (inheriting.value ? props.inherited || [] : props.modelValue || []))
const selectedSet = computed(() => new Set(selected.value))
const byId = computed(() => new Map(repos.value.map((r) => [r.id, r])))

type Problem = '' | 'noCredential' | 'mismatch'
function problemOf(r: Project): Problem {
  if (!r.gitCredentialType || !r.gitCredentialId) return 'noCredential'
  const want = r.gitCredentialType === 'ssh' ? 'ssh' : 'https'
  if (repoProtocol(r.repoUrl) !== want) return 'mismatch'
  return ''
}

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  const list = q
    ? repos.value.filter((r) => r.name.toLowerCase().includes(q) || r.repoUrl.toLowerCase().includes(q))
    : repos.value
  return [...list].sort((a, b) => Number(!!problemOf(a)) - Number(!!problemOf(b)) || a.name.localeCompare(b.name))
})
const missingIds = computed(() => (loading.value || loadError.value ? [] : selected.value.filter((id) => !byId.value.has(id))))
const inheritedRepos = computed(() => selected.value.map((id) => byId.value.get(id)).filter((r): r is Project => !!r))
const usable = computed(() => repos.value.filter((r) => !problemOf(r)))
const showSearch = computed(() => repos.value.length > 6)

function set(ids: string[]) {
  emit('update:modelValue', ids)
}
function toggle(r: Project) {
  if (inheriting.value) return
  const cur = props.modelValue || []
  set(cur.includes(r.id) ? cur.filter((id) => id !== r.id) : [...cur, r.id])
}
function removeMissing(id: string) {
  set((props.modelValue || []).filter((x) => x !== id))
}
function selectAll() {
  const cur = props.modelValue || []
  set([...cur, ...usable.value.map((r) => r.id).filter((id) => !cur.includes(id))])
}
function setInherit(on: boolean) {
  emit('update:modelValue', on ? undefined : [...(props.inherited || [])])
}
</script>

<template>
  <section class="border-t border-dashed border-line pt-4" data-testid="repo-access">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="flex items-center gap-2 text-[12px] font-medium text-txt2">
          {{ t('pages.agentStudio.repoAccess.title') }}
          <span
            v-if="selected.length"
            class="rounded-full bg-accent-dim px-1.5 py-px text-[10px] font-semibold text-accent-2"
            data-testid="repo-access-count"
          >{{ t('pages.agentStudio.repoAccess.selected', { n: selected.length }) }}</span>
        </div>
        <p class="mt-1 text-[11px] leading-5 text-txt3">
          {{ shared ? t('pages.agentStudio.repoAccess.sharedDesc') : t('pages.agentStudio.repoAccess.desc') }}
        </p>
      </div>
      <a
        href="/settings/projects"
        class="shrink-0 whitespace-nowrap text-[11px] text-txt3 transition hover:text-accent-2"
        data-testid="repo-access-manage"
        @click.prevent="go('/settings/projects')"
      >
        {{ t('pages.agentStudio.repoAccess.manage') }}
      </a>
    </div>

    <div
      v-if="inheritable"
      class="mt-3 flex items-center justify-between gap-4 rounded-lg border border-line bg-base px-3 py-2.5"
    >
      <div class="min-w-0">
        <div class="text-[12px] font-medium text-txt">{{ t('pages.agentStudio.repoAccess.inherit') }}</div>
        <div class="text-[11px] text-txt3">{{ t('pages.agentStudio.repoAccess.inheritHint') }}</div>
      </div>
      <AppSwitch
        :model-value="inheriting"
        data-testid="repo-access-inherit"
        :aria-label="t('pages.agentStudio.repoAccess.inherit')"
        @update:model-value="setInherit"
      />
    </div>

    <div v-if="loading" class="mt-3 space-y-1.5" aria-busy="true">
      <div v-for="i in 3" :key="i" class="h-11 animate-pulse rounded-lg bg-elevated/60" />
    </div>

    <div
      v-else-if="loadError"
      class="mt-3 flex items-center justify-between gap-3 rounded-lg border border-err/30 bg-err/5 px-3 py-2.5 text-[12px] text-err"
    >
      {{ t('pages.agentStudio.repoAccess.loadFailed') }}
      <button type="button" class="text-[12px] font-medium underline-offset-2 hover:underline" @click="load">
        {{ t('pages.agentStudio.repoAccess.retry') }}
      </button>
    </div>

    <template v-else-if="inheriting">
      <div v-if="inheritedRepos.length" class="mt-3 flex flex-wrap gap-1.5" data-testid="repo-access-inherited">
        <span
          v-for="r in inheritedRepos"
          :key="r.id"
          class="inline-flex max-w-full items-center gap-1.5 rounded-md border border-line bg-base px-2 py-1 text-[11px] text-txt2"
        >
          <Icon name="git" :size="12" class="shrink-0 text-txt3" />
          <span class="truncate font-medium text-txt">{{ r.name }}</span>
          <span class="truncate font-mono text-[10px] text-txt3">{{ repoShort(r.repoUrl) }}</span>
        </span>
      </div>
      <p v-else class="mt-3 text-[11px] text-txt3">{{ t('pages.agentStudio.repoAccess.inheritedEmpty') }}</p>
    </template>

    <div
      v-else-if="!repos.length"
      class="mt-3 flex flex-col items-center gap-2 rounded-lg border border-dashed border-line px-4 py-6 text-center"
      data-testid="repo-access-empty"
    >
      <Icon name="git" :size="20" class="text-txt3" />
      <div class="text-[12px] font-medium text-txt">{{ t('pages.agentStudio.repoAccess.empty') }}</div>
      <p class="max-w-sm text-[11px] leading-5 text-txt3">{{ t('pages.agentStudio.repoAccess.emptyHint') }}</p>
      <a href="/settings/projects?new=1" class="chip mt-1 text-[11px]" @click.prevent="go('/settings/projects?new=1')">
        {{ t('pages.agentStudio.repoAccess.add') }}
      </a>
    </div>

    <template v-else>
      <div class="mt-3 flex items-center gap-2">
        <input
          v-if="showSearch"
          v-model="query"
          type="search"
          class="input h-8 min-w-0 flex-1 text-[12px]"
          :placeholder="t('pages.agentStudio.repoAccess.search')"
          data-testid="repo-access-search"
        />
        <div class="ml-auto flex shrink-0 items-center gap-3 text-[11px]">
          <button
            type="button"
            class="text-txt3 transition hover:text-accent-2 disabled:opacity-40 disabled:hover:text-txt3"
            :disabled="usable.every((r) => selectedSet.has(r.id))"
            data-testid="repo-access-select-all"
            @click="selectAll"
          >
            {{ t('pages.agentStudio.repoAccess.selectAll') }}
          </button>
          <button
            type="button"
            class="text-txt3 transition hover:text-accent-2 disabled:opacity-40 disabled:hover:text-txt3"
            :disabled="!selected.length"
            data-testid="repo-access-clear"
            @click="set([])"
          >
            {{ t('pages.agentStudio.repoAccess.clear') }}
          </button>
        </div>
      </div>

      <ul class="mt-2 max-h-80 space-y-1 overflow-y-auto pr-0.5" role="group" :aria-label="t('pages.agentStudio.repoAccess.title')">
        <li v-for="id in missingIds" :key="`missing-${id}`">
          <div
            class="flex items-center gap-3 rounded-lg border border-warn/35 bg-warn/5 px-3 py-2"
            data-testid="repo-access-missing"
          >
            <Icon name="alert" :size="14" class="shrink-0 text-warn" />
            <div class="min-w-0 flex-1">
              <div class="text-[12px] font-medium text-txt">{{ t('pages.agentStudio.repoAccess.deleted') }}</div>
              <div class="truncate font-mono text-[10px] text-txt3">{{ id }}</div>
            </div>
            <button type="button" class="text-[11px] text-warn hover:underline" @click="removeMissing(id)">
              {{ t('pages.agentStudio.repoAccess.remove') }}
            </button>
          </div>
        </li>
        <li v-for="r in filtered" :key="r.id">
          <label
            class="group flex items-center gap-3 rounded-lg border px-3 py-2 transition"
            :class="[
              selectedSet.has(r.id) ? 'border-accent/50 bg-accent-dim' : 'border-line bg-base',
              problemOf(r) && !selectedSet.has(r.id) ? 'cursor-not-allowed opacity-60' : 'cursor-pointer hover:border-line-strong',
            ]"
            :data-testid="`repo-access-${r.id}`"
          >
            <input
              type="checkbox"
              class="h-3.5 w-3.5 shrink-0 accent-[rgb(var(--c-accent))]"
              :checked="selectedSet.has(r.id)"
              :disabled="!!problemOf(r) && !selectedSet.has(r.id)"
              @change="toggle(r)"
            />
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="truncate text-[12px] font-medium text-txt">{{ r.name }}</span>
                <span
                  v-if="r.gitCredentialType && problemOf(r) !== 'noCredential'"
                  class="shrink-0 rounded border border-line px-1 py-px font-mono text-[9px] uppercase tracking-wide text-txt3"
                >{{ t(`pages.agentStudio.repoAccess.access.${r.gitCredentialType}`) }}</span>
              </div>
              <div class="truncate font-mono text-[10px] text-txt3">{{ repoShort(r.repoUrl) }}</div>
            </div>
            <a
              v-if="problemOf(r)"
              :href="`/settings/projects?edit=${encodeURIComponent(r.id)}`"
              class="shrink-0 text-[10px] text-warn hover:underline"
              :title="t('pages.agentStudio.repoAccess.noCredentialHint')"
              @click.stop.prevent="go(`/settings/projects?edit=${encodeURIComponent(r.id)}`)"
            >
              {{ t(`pages.agentStudio.repoAccess.${problemOf(r)}`) }}
            </a>
          </label>
        </li>
        <li v-if="!filtered.length" class="px-3 py-4 text-center text-[11px] text-txt3">
          {{ t('pages.agentStudio.repoAccess.noMatch') }}
        </li>
      </ul>
    </template>
  </section>
</template>
