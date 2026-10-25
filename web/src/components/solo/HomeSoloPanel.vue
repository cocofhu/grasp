<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import SoloStatusDot from './SoloStatusDot.vue'
import { relTime } from '@/lib/shared/format'
import { repoShort } from '@/lib/solo/soloPrefs'
import { sessionRepoLabel } from '@/lib/solo/sessionRepos'
import { useSoloSessions } from '@/lib/solo/useSoloSessions'
import type { Project } from '@/lib/api/apiTypes'

const props = withDefaults(defineProps<{ projects?: Project[]; projectIds?: string[] }>(), {
  projects: () => [],
  projectIds: () => [],
})
const emit = defineEmits<{ (e: 'toggle-project', id: string): void; (e: 'new-project'): void }>()
const { t } = useI18n()
const store = useSoloSessions()

const recent = computed(() => store.active.value.slice(0, 3))
const cards = computed(() => props.projects.slice(0, 8))
const selected = computed(() => (Array.isArray(props.projectIds) ? props.projectIds : []))

function repoMeta(session: (typeof recent.value)[number]) {
  const label = sessionRepoLabel(session)
  return label ? `${session.agentName} · ${label}` : session.agentName
}
</script>

<template>
  <div class="home-solo w-full" data-testid="home-solo-panel">
    <section v-if="recent.length" class="home-solo__section">
      <div class="home-solo__head">
        <span>{{ t('pages.solo.home.recent') }}</span>
        <span class="home-solo__count">{{ store.active.value.length }}/{{ store.maxSandboxes.value }}</span>
        <RouterLink to="/solo" class="home-solo__more">{{ t('pages.solo.home.viewAll') }}<Icon name="chevron-right" :size="12" /></RouterLink>
      </div>
      <div class="home-solo__list">
        <RouterLink
          v-for="s in recent"
          :key="s.id"
          :to="`/solo/${s.id}`"
          class="home-solo__row"
          :data-testid="`home-solo-session-${s.id}`"
        >
          <SoloStatusDot :status="s.status" :busy="s.busy" />
          <span class="min-w-0 flex-1 truncate text-[13px] text-txt">{{ s.title || t('pages.solo.untitled') }}</span>
          <span class="hidden min-w-0 max-w-[40%] truncate text-[11.5px] text-txt3 sm:block">
            {{ repoMeta(s) }}
          </span>
          <span class="shrink-0 text-[11px] text-txt3">{{ relTime(s.lastActiveAt || s.createdAt) }}</span>
        </RouterLink>
      </div>
    </section>

    <section class="home-solo__section">
      <div class="home-solo__head">
        <span>{{ t('pages.solo.home.projects') }}</span>
        <RouterLink to="/settings/projects" class="home-solo__more">{{ t('pages.solo.projectSelect.manage') }}<Icon name="chevron-right" :size="12" /></RouterLink>
      </div>
      <div class="home-solo__cards">
        <button
          v-for="p in cards"
          :key="p.id"
          type="button"
          class="home-solo__card"
          :class="{ 'home-solo__card--on': selected.includes(p.id) }"
          :aria-pressed="selected.includes(p.id)"
          :data-testid="`home-solo-project-${p.id}`"
          @click="emit('toggle-project', p.id)"
        >
          <span class="flex items-center gap-1.5">
            <Icon name="git" :size="13" class="shrink-0 text-accent" />
            <span class="truncate text-[13px] font-medium text-txt">{{ p.name }}</span>
          </span>
          <span class="truncate font-mono text-[11px] text-txt3">{{ repoShort(p.repoUrl) }}</span>
          <span class="text-[10.5px] text-txt3">
            {{ p.lastUsedAt ? relTime(p.lastUsedAt) : t('pages.projects.neverUsed') }}
          </span>
        </button>
        <button type="button" class="home-solo__card home-solo__card--add" data-testid="home-solo-new-project" @click="emit('new-project')">
          <Icon name="plus" :size="15" />
          <span class="text-[12.5px]">{{ t('pages.solo.projectSelect.create') }}</span>
        </button>
      </div>
      <p v-if="!projects.length" class="mt-2 text-[12px] text-txt3">{{ t('pages.solo.home.projectsEmpty') }}</p>
    </section>
  </div>
</template>

<style scoped>
.home-solo {
  display: flex;
  flex-direction: column;
  gap: 22px;
  margin-top: 28px;
  padding-bottom: 32px;
  animation: home-solo-in 0.28s cubic-bezier(0.2, 0.7, 0.2, 1) both;
}
.home-solo__head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  color: rgb(var(--c-txt3));
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.04em;
}
.home-solo__count {
  border-radius: 999px;
  background: rgb(var(--c-elevated));
  padding: 0 6px;
  font-size: 10.5px;
  font-weight: 500;
}
.home-solo__more {
  display: inline-flex;
  align-items: center;
  margin-left: auto;
  font-weight: 500;
}
.home-solo__more:hover {
  color: rgb(var(--c-accent-2));
}
.home-solo__list {
  overflow: hidden;
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  background: rgb(var(--c-surface) / 0.75);
}
.home-solo__row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  transition: background 0.14s ease;
}
.home-solo__row + .home-solo__row {
  border-top: 1px solid rgb(var(--c-line) / 0.6);
}
.home-solo__row:hover {
  background: rgb(var(--c-accent) / 0.05);
}
.home-solo__cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(168px, 1fr));
  gap: 10px;
}
.home-solo__card {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  background: rgb(var(--c-surface) / 0.75);
  padding: 10px 12px;
  text-align: left;
  transition: border-color 0.15s ease, background 0.15s ease, transform 0.15s ease;
}
.home-solo__card:hover {
  border-color: rgb(var(--c-accent) / 0.3);
  transform: translateY(-1px);
}
.home-solo__card--on {
  border-color: rgb(var(--c-accent) / 0.6);
  background: rgb(var(--c-accent) / 0.07);
}
.home-solo__card--add {
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-style: dashed;
  color: rgb(var(--c-txt2));
  flex-direction: row;
  min-height: 70px;
}
@keyframes home-solo-in {
  from {
    opacity: 0;
    transform: translateY(6px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}
@media (prefers-reduced-motion: reduce) {
  .home-solo,
  .home-solo__card {
    animation: none;
    transition: none;
  }
}
</style>
