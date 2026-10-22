<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import SoloStatusDot from '@/components/solo/SoloStatusDot.vue'
import SoloSessionPane from '@/components/solo/SoloSessionPane.vue'
import SoloCreateModal from '@/components/solo/SoloCreateModal.vue'
import { relTime } from '@/lib/shared/format'
import { isSoloActive, useSoloSessions } from '@/lib/solo/useSoloSessions'
import { soloPrefs } from '@/lib/solo/soloPrefs'

const LIST_REFRESH_MS = 15_000

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useSoloSessions()

const filter = ref<'active' | 'all'>('active')
const createOpen = ref(false)
const presetAgent = ref('')
const presetProject = ref('')
let timer: ReturnType<typeof setInterval> | undefined

const sessionId = computed(() => (typeof route.params.id === 'string' ? route.params.id : ''))
const listed = computed(() => {
  const all = store.sessions.value
  const rows = filter.value === 'active' ? all.filter(isSoloActive) : all
  return [...rows].sort((a, b) => Date.parse(b.lastActiveAt || b.createdAt) - Date.parse(a.lastActiveAt || a.createdAt))
})
const activeCount = computed(() => store.active.value.length)

// ?new=1&agent=x&project=y opens the dialog prefilled (Agent Studio, project list).
watch(
  () => route.query.new,
  (v) => {
    if (v !== '1') return
    presetAgent.value = typeof route.query.agent === 'string' ? route.query.agent : ''
    presetProject.value = typeof route.query.project === 'string' ? route.query.project : ''
    createOpen.value = true
    void router.replace({ path: route.path })
  },
  { immediate: true },
)

watch(
  () => store.loaded.value,
  (ok) => {
    // A deep link to a closed session should still show it in the list.
    if (ok && sessionId.value && !store.active.value.some((s) => s.id === sessionId.value)) filter.value = 'all'
    if (ok && !store.active.value.length && store.sessions.value.length) filter.value = 'all'
  },
)

function goHome() {
  soloPrefs.setHomeMode('solo')
  void router.replace('/dashboard')
}

// Solo starts from Home. /solo without a session is only the list / create dialog.
watch(
  () => [store.loaded.value, sessionId.value, createOpen.value, route.query.new] as const,
  ([ok, id, creating, newFlag]) => {
    if (!ok || id || creating || newFlag === '1') return
    const first = store.active.value[0] ?? store.sessions.value[0]
    if (first) {
      void router.replace(`/solo/${first.id}`)
      return
    }
    goHome()
  },
)

function openCreate() {
  presetAgent.value = ''
  presetProject.value = ''
  createOpen.value = true
}

onMounted(() => {
  void store.refresh()
  timer = setInterval(() => void store.refresh(), LIST_REFRESH_MS)
})
onBeforeUnmount(() => timer && clearInterval(timer))
</script>

<template>
  <div class="solo-view flex h-full min-h-0" :class="{ 'solo-view--detail': !!sessionId }" data-testid="solo-view">
    <aside class="solo-list" :aria-label="t('pages.solo.title')">
      <div class="solo-list__head">
        <div class="min-w-0 flex-1">
          <h1 class="text-[15px] font-semibold text-txt">{{ t('pages.solo.title') }}</h1>
          <p class="truncate text-[11.5px] text-txt3">{{ t('pages.solo.capacity', { n: activeCount, max: store.maxSandboxes.value }) }}</p>
        </div>
        <AppButton size="sm" variant="primary" icon="plus" data-testid="solo-new" @click="openCreate">{{ t('pages.solo.newSession') }}</AppButton>
      </div>
      <div class="solo-list__tabs" role="tablist">
        <button
          v-for="f in (['active', 'all'] as const)"
          :key="f"
          type="button"
          role="tab"
          class="solo-list__tab"
          :class="{ 'is-on': filter === f }"
          :aria-selected="filter === f"
          @click="filter = f"
        >
          {{ f === 'active' ? t('pages.solo.filterActive') : t('pages.solo.filterAll') }}
          <span class="solo-list__tab-count">{{ f === 'active' ? activeCount : store.sessions.value.length }}</span>
        </button>
      </div>
      <div class="solo-list__body scroll-area">
        <div v-if="!store.loaded.value" class="py-8 text-center text-txt3"><Icon name="spinner" :size="15" class="inline animate-spin" /></div>
        <div v-else-if="!listed.length" class="solo-list__empty">
          <Icon name="terminal" :size="20" class="text-txt3" />
          <p class="text-[12.5px] text-txt2">{{ filter === 'active' ? t('pages.solo.emptyActive') : t('pages.solo.emptyList') }}</p>
          <button v-if="filter === 'active' && store.sessions.value.length" type="button" class="text-[12px] text-accent-2 hover:underline" @click="filter = 'all'">
            {{ t('pages.solo.showAll') }}
          </button>
        </div>
        <RouterLink
          v-for="s in listed"
          :key="s.id"
          :to="`/solo/${s.id}`"
          class="solo-row"
          :class="{ 'is-on': s.id === sessionId, 'is-closed': s.status === 'closed' }"
          :data-testid="`solo-row-${s.id}`"
        >
          <SoloStatusDot :status="s.status" :busy="s.busy" class="mt-1.5" />
          <span class="min-w-0 flex-1">
            <span class="flex items-center gap-2">
              <span class="min-w-0 flex-1 truncate text-[13px] text-txt">{{ s.title || t('pages.solo.untitled') }}</span>
              <span class="shrink-0 text-[10.5px] text-txt3">{{ relTime(s.lastActiveAt || s.createdAt) }}</span>
            </span>
            <span class="mt-0.5 flex items-center gap-1 text-[11.5px] text-txt3">
              <span class="truncate">{{ s.agentName }}</span>
              <template v-if="s.project">
                <span>·</span>
                <Icon name="git" :size="11" class="shrink-0" />
                <span class="truncate">{{ s.project.name }}</span>
              </template>
              <span v-if="s.waiting" class="ml-auto shrink-0 rounded-full bg-elevated px-1.5 text-[10px]">+{{ s.waiting }}</span>
            </span>
          </span>
        </RouterLink>
      </div>
    </aside>

    <main class="solo-main min-h-0 min-w-0 flex-1">
      <SoloSessionPane v-if="sessionId" :key="sessionId" :session-id="sessionId" @back="goHome" />
    </main>

    <SoloCreateModal :open="createOpen" :preset-agent="presetAgent" :preset-project="presetProject" @close="createOpen = false" />
  </div>
</template>

<style scoped>
.solo-view {
  overflow: hidden;
  border: 1px solid rgb(var(--c-line));
  border-radius: 14px;
  background: rgb(var(--c-surface));
}
.solo-list {
  display: flex;
  width: 300px;
  flex-shrink: 0;
  flex-direction: column;
  border-right: 1px solid rgb(var(--c-line));
}
.solo-list__head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 14px 10px;
}
.solo-list__tabs {
  display: flex;
  gap: 4px;
  padding: 0 14px 8px;
}
.solo-list__tab {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border-radius: 7px;
  padding: 3px 9px;
  color: rgb(var(--c-txt3));
  font-size: 12px;
}
.solo-list__tab.is-on {
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt));
}
.solo-list__tab-count {
  font-size: 10.5px;
  opacity: 0.75;
}
.solo-list__body {
  min-height: 0;
  flex: 1;
  overflow-y: auto;
  padding: 0 8px 12px;
}
.solo-list__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 36px 12px;
  text-align: center;
}
.solo-row {
  display: flex;
  gap: 10px;
  border-radius: 10px;
  padding: 9px 10px;
  transition: background 0.14s ease;
}
.solo-row:hover {
  background: rgb(var(--c-elevated) / 0.7);
}
.solo-row.is-on {
  background: rgb(var(--c-accent) / 0.09);
}
.solo-row.is-closed {
  opacity: 0.7;
}
.solo-main {
  display: flex;
}
@media (max-width: 860px) {
  .solo-list {
    width: 100%;
    border-right: 0;
  }
  .solo-main {
    display: none;
  }
  .solo-view--detail .solo-list {
    display: none;
  }
  .solo-view--detail .solo-main {
    display: flex;
  }
}
</style>
