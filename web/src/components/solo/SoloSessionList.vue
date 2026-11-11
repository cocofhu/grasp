<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import SoloStatusDot from './SoloStatusDot.vue'
import { relTime } from '@/lib/shared/format'
import { sessionRepoLabel } from '@/lib/solo/sessionRepos'
import { isSoloActive, useSoloSessions } from '@/lib/solo/useSoloSessions'
import type { SoloSession } from '@/lib/api/apiTypes'

defineProps<{ embedded?: boolean }>()
const emit = defineEmits<{ (e: 'create'): void; (e: 'open', id: string): void }>()
const { t } = useI18n()
const store = useSoloSessions()

const filter = ref<'active' | 'all'>('active')
const activeCount = computed(() => store.active.value.length)
const computeCount = computed(() => store.active.value.filter(session => session.status === 'starting' || session.status === 'running').length)
const listed = computed(() => {
  const all = store.sessions.value
  const rows = filter.value === 'active' ? all.filter(isSoloActive) : all
  return [...rows].sort((a, b) => Date.parse(b.lastActiveAt || b.createdAt) - Date.parse(a.lastActiveAt || a.createdAt))
})

watch(
  () => store.loaded.value,
  (ok) => {
    if (ok && !store.active.value.length && store.sessions.value.length) filter.value = 'all'
  },
  { immediate: true },
)

function repoMeta(session: SoloSession) {
  const label = sessionRepoLabel(session)
  return label ? `${session.agentName} · ${label}` : session.agentName
}
</script>

<template>
  <section class="solo-list" :class="{ 'solo-list--embedded': embedded }" data-testid="solo-list">
    <header v-if="!embedded" class="solo-list__head">
      <div class="min-w-0 flex-1">
        <h1 class="text-[15px] font-semibold text-txt">{{ t('pages.solo.title') }}</h1>
        <p class="truncate text-[11.5px] text-txt3">{{ t('pages.solo.capacity', { n: computeCount, max: store.maxSandboxes.value }) }}</p>
      </div>
      <AppButton size="sm" variant="primary" icon="plus" data-testid="solo-new" @click="emit('create')">{{ t('pages.solo.newSession') }}</AppButton>
    </header>

    <div class="solo-list__tabs" role="tablist">
      <button
        v-for="f in (['active', 'all'] as const)"
        :key="f"
        type="button"
        role="tab"
        class="solo-list__tab"
        :class="{ 'is-on': filter === f }"
        :aria-selected="filter === f"
        :data-testid="`solo-filter-${f}`"
        @click="filter = f"
      >
        {{ f === 'active' ? t('pages.solo.filterActive') : t('pages.solo.filterAll') }}
        <span class="solo-list__tab-count">{{ f === 'active' ? activeCount : store.sessions.value.length }}</span>
      </button>
      <span v-if="embedded" class="ml-auto self-center truncate text-[11.5px] text-txt3">{{ t('pages.solo.capacity', { n: computeCount, max: store.maxSandboxes.value }) }}</span>
    </div>

    <div v-if="!store.loaded.value" class="py-8 text-center text-txt3"><Icon name="spinner" :size="15" class="inline animate-spin" /></div>
    <div v-else-if="!listed.length" class="solo-list__empty">
      <Icon name="terminal" :size="20" class="text-txt3" />
      <p class="text-[12.5px] text-txt2">{{ filter === 'active' ? t('pages.solo.emptyActive') : t('pages.solo.emptyList') }}</p>
      <button v-if="filter === 'active' && store.sessions.value.length" type="button" class="text-[12px] text-accent-2 hover:underline" @click="filter = 'all'">
        {{ t('pages.solo.showAll') }}
      </button>
    </div>
    <div v-else class="solo-list__rows">
      <RouterLink
        v-for="s in listed"
        :key="s.id"
        :to="`/solo/${s.id}`"
        class="solo-list__row"
        :class="{ 'is-ended': !isSoloActive(s) }"
        :data-testid="`solo-row-${s.id}`"
        @click="emit('open', s.id)"
      >
        <SoloStatusDot :status="s.status" :busy="s.busy" />
        <span class="min-w-0 flex-1 truncate text-[13px] text-txt">{{ s.title || t('pages.solo.untitled') }}</span>
        <span v-if="s.waiting" class="shrink-0 rounded-full bg-elevated px-1.5 text-[10px] text-txt3">+{{ s.waiting }}</span>
        <span class="hidden min-w-0 max-w-[40%] truncate text-[11.5px] text-txt3 sm:block">{{ repoMeta(s) }}</span>
        <span class="shrink-0 text-[11px] text-txt3">{{ relTime(s.lastActiveAt || s.createdAt) }}</span>
      </RouterLink>
    </div>
  </section>
</template>

<style scoped>
.solo-list {
  width: 100%;
  max-width: 820px;
  margin: 0 auto;
  padding: 28px 20px 32px;
}
.solo-list--embedded {
  max-width: none;
  padding: 0;
}
.solo-list__head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}
.solo-list__tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 10px;
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
.solo-list__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 36px 12px;
  text-align: center;
}
.solo-list__rows {
  overflow: hidden;
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  background: rgb(var(--c-surface) / 0.75);
}
.solo-list__row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  transition: background 0.14s ease;
}
.solo-list__row + .solo-list__row {
  border-top: 1px solid rgb(var(--c-line) / 0.6);
}
.solo-list__row:hover {
  background: rgb(var(--c-accent) / 0.05);
}
.solo-list__row.is-ended {
  opacity: 0.7;
}
</style>
