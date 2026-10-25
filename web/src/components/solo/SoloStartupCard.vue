<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { repoShort } from '@/lib/solo/soloPrefs'
import { sessionRepos } from '@/lib/solo/sessionRepos'
import type { SoloSession } from '@/lib/api/apiTypes'

/** Boot progress of a session's sandbox: pulling → container → clone → agent. */
const props = defineProps<{ session: SoloSession }>()
const { t } = useI18n()

const steps = computed(() => {
  const out = [
    { id: 'pulling', title: t('pages.solo.startup.pulling') },
    { id: 'creating', title: t('pages.solo.startup.creating') },
  ]
  sessionRepos(props.session).forEach((repo, index) => {
    const short = repoShort(repo.repoUrl)
    if (!short) return
    out.push({ id: `clone-${index}`, title: t('pages.solo.startup.clone', { repo: short }) })
  })
  out.push({ id: 'agent', title: t('pages.solo.startup.agent', { agent: props.session.agentName }) })
  return out
})

// Container phases after "creating" are not reported; advance on a timer and
// hold on the last step until the session flips to running.
const tick = ref(0)
let timer: ReturnType<typeof setInterval> | undefined

const sandboxStatus = computed(() => props.session.sandbox?.status ?? '')
const current = computed(() => {
  const s = sandboxStatus.value
  const last = steps.value.length - 1
  if (s === 'pulling') return 0
  if (s === 'running') return last
  if (!s) return 1
  return Math.min(1 + tick.value, last)
})

watch(
  sandboxStatus,
  (s) => {
    if (timer) clearInterval(timer)
    timer = undefined
    tick.value = 0
    if (s !== 'pulling' && s !== 'running') timer = setInterval(() => tick.value++, 3500)
  },
  { immediate: true },
)
onBeforeUnmount(() => timer && clearInterval(timer))
</script>

<template>
  <div class="solo-startup" data-testid="solo-startup-card" aria-live="polite">
    <div class="flex items-center gap-2">
      <Icon name="spinner" :size="15" class="text-accent" />
      <span class="text-[13px] font-medium text-txt">{{ t('pages.solo.startup.title') }}</span>
      <span v-if="session.sandbox?.name" class="ml-auto truncate font-mono text-[11px] text-txt3">{{ session.sandbox.name }}</span>
    </div>
    <ol class="solo-startup__steps">
      <li
        v-for="(s, i) in steps"
        :key="s.id"
        class="solo-startup__step"
        :class="{ 'is-done': i < current, 'is-on': i === current }"
      >
        <span class="solo-startup__mark">
          <Icon v-if="i < current" name="check" :size="11" />
          <span v-else-if="i === current" class="solo-startup__pulse" />
        </span>
        <span class="truncate">{{ s.title }}</span>
      </li>
    </ol>
    <p class="text-[11.5px] leading-relaxed text-txt3">{{ t('pages.solo.startup.hint') }}</p>
  </div>
</template>

<style scoped>
.solo-startup {
  display: flex;
  flex-direction: column;
  gap: 12px;
  border: 1px solid rgb(var(--c-accent) / 0.2);
  border-radius: 14px;
  background: linear-gradient(180deg, rgb(var(--c-accent) / 0.05), transparent);
  padding: 14px 16px;
}
.solo-startup__steps {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.solo-startup__step {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 9px;
  color: rgb(var(--c-txt3));
  font-size: 12.5px;
  transition: color 0.2s ease;
}
.solo-startup__step.is-on {
  color: rgb(var(--c-txt));
}
.solo-startup__step.is-done {
  color: rgb(var(--c-txt2));
}
.solo-startup__mark {
  display: inline-flex;
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border: 1px solid rgb(var(--c-line));
  border-radius: 999px;
}
.is-done .solo-startup__mark {
  border-color: rgb(var(--c-ok) / 0.5);
  background: rgb(var(--c-ok) / 0.12);
  color: rgb(var(--c-ok));
}
.is-on .solo-startup__mark {
  border-color: rgb(var(--c-accent) / 0.6);
}
.solo-startup__pulse {
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: rgb(var(--c-accent));
  animation: solo-startup-pulse 1.2s ease-in-out infinite;
}
@keyframes solo-startup-pulse {
  50% {
    opacity: 0.3;
    transform: scale(0.7);
  }
}
@media (prefers-reduced-motion: reduce) {
  .solo-startup__pulse {
    animation: none;
  }
}
</style>
