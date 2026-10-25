<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppDrawer from '@/components/ui/AppDrawer.vue'
import Icon from '@/components/ui/Icon.vue'
import { api } from '@/lib/api/api'
import type { SoloEnvEntry, SoloEnvSource, SoloSession } from '@/lib/api/apiTypes'
import { backendLabel, repoShort } from '@/lib/solo/soloPrefs'
import { branchOfRepo, sessionRepos } from '@/lib/solo/sessionRepos'

/** What the sandbox was started with. Values are never shown, only names and origin. */
const props = defineProps<{ open: boolean; session: SoloSession }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const { t } = useI18n()

const entries = ref<SoloEnvEntry[]>([])
const loading = ref(false)
const filter = ref<SoloEnvSource | ''>('')

const SOURCES: SoloEnvSource[] = ['project', 'credential', 'agent', 'platform', 'system']

watch(
  () => [props.open, props.session.sandboxId] as const,
  async ([open]) => {
    if (!open) return
    loading.value = true
    try {
      entries.value = await api.getSoloEnv(props.session.id)
    } catch {
      entries.value = []
    } finally {
      loading.value = false
    }
  },
  { immediate: true },
)

const counts = computed(() => {
  const out: Record<string, number> = {}
  for (const e of entries.value) out[e.source] = (out[e.source] ?? 0) + 1
  return out
})
const shown = computed(() =>
  [...entries.value]
    .filter((e) => !filter.value || e.source === filter.value)
    .sort((a, b) => SOURCES.indexOf(a.source) - SOURCES.indexOf(b.source) || a.key.localeCompare(b.key)),
)

const repos = computed(() => sessionRepos(props.session))
const facts = computed(() => {
  const rows: { label: string; value: string; mono?: boolean }[] = [
    { label: t('pages.solo.env.agent'), value: props.session.agentName },
    { label: t('pages.solo.env.backend'), value: backendLabel(props.session.backend) },
  ]
  if (props.session.model) rows.push({ label: t('pages.solo.env.model'), value: props.session.model, mono: true })
  return rows
})
</script>

<template>
  <AppDrawer :open="open" :title="t('pages.solo.env.title')" :width="440" @close="emit('close')">
    <div class="space-y-5 p-5" data-testid="solo-env-panel">
      <dl class="solo-env__facts">
        <template v-for="f in facts" :key="f.label">
          <dt>{{ f.label }}</dt>
          <dd :class="{ 'font-mono': f.mono }">{{ f.value }}</dd>
        </template>
      </dl>
      <p v-if="!repos.length" class="text-[11.5px] text-txt3">{{ t('pages.solo.noProject') }}</p>
      <section v-for="repo in repos" :key="`${repo.position}-${repo.projectId}`" class="solo-env__repo" :data-testid="`solo-env-repo-${repo.projectId || repo.position}`">
        <h3 class="mb-2 text-[13px] font-medium text-txt">{{ repo.name }}</h3>
        <dl class="solo-env__facts">
          <dt>{{ t('pages.solo.env.repo') }}</dt>
          <dd class="font-mono">{{ repoShort(repo.repoUrl) }}</dd>
          <dt>{{ t('pages.solo.env.branch') }}</dt>
          <dd class="font-mono">{{ branchOfRepo(repo, t('pages.solo.env.remoteDefault')) }}</dd>
          <dt>{{ t('pages.solo.env.dir') }}</dt>
          <dd class="font-mono">{{ repo.repoDirName }}</dd>
          <dt>{{ t('pages.solo.env.access') }}</dt>
          <dd>{{ t(`pages.solo.access.${repo.gitCredentialType || 'none'}`) }}</dd>
        </dl>
      </section>
      <p v-if="repos.length" class="text-[11.5px] text-txt3">{{ t('pages.solo.env.snapshotHint') }}</p>

      <section>
        <div class="mb-2 flex items-center gap-2">
          <span class="text-[11px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.solo.env.vars') }}</span>
          <span class="text-[11px] text-txt3">{{ entries.length }}</span>
        </div>
        <p class="mb-3 text-[11.5px] text-txt3">{{ t('pages.solo.env.hint') }}</p>
        <div class="mb-3 flex flex-wrap gap-1.5">
          <button type="button" class="solo-env__chip" :class="{ 'is-on': !filter }" @click="filter = ''">
            {{ t('pages.solo.env.all') }}
          </button>
          <button
            v-for="s in SOURCES.filter((x) => counts[x])"
            :key="s"
            type="button"
            class="solo-env__chip"
            :class="[`solo-env__chip--${s}`, { 'is-on': filter === s }]"
            @click="filter = filter === s ? '' : s"
          >
            {{ t(`pages.solo.env.source.${s}`) }} · {{ counts[s] }}
          </button>
        </div>
        <div v-if="loading" class="py-6 text-center text-txt3"><Icon name="spinner" :size="14" class="inline" /></div>
        <p v-else-if="!entries.length" class="py-6 text-center text-[12px] text-txt3">{{ t('pages.solo.env.empty') }}</p>
        <ul v-else class="divide-y divide-line/60 rounded-lg border border-line">
          <li v-for="e in shown" :key="e.key" class="flex items-center gap-2 px-3 py-1.5">
            <span class="min-w-0 flex-1 truncate font-mono text-[12px] text-txt">{{ e.key }}</span>
            <span class="solo-env__badge" :class="`solo-env__chip--${e.source}`">{{ t(`pages.solo.env.source.${e.source}`) }}</span>
          </li>
        </ul>
      </section>
    </div>
  </AppDrawer>
</template>

<style scoped>
.solo-env__facts {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px 16px;
  font-size: 12.5px;
}
.solo-env__facts dt {
  color: rgb(var(--c-txt3));
}
.solo-env__facts dd {
  min-width: 0;
  overflow: hidden;
  color: rgb(var(--c-txt));
  text-overflow: ellipsis;
  white-space: nowrap;
}
.solo-env__repo {
  margin-top: 14px;
  border-top: 1px solid rgb(var(--c-line) / 0.7);
  padding-top: 12px;
}
.solo-env__chip {
  border: 1px solid rgb(var(--c-line));
  border-radius: 999px;
  padding: 1px 9px;
  color: rgb(var(--c-txt2));
  font-size: 11px;
}
.solo-env__chip.is-on {
  border-color: rgb(var(--c-accent) / 0.5);
  background: rgb(var(--c-accent) / 0.1);
  color: rgb(var(--c-accent-2));
}
.solo-env__badge {
  flex-shrink: 0;
  border-radius: 999px;
  padding: 0 7px;
  font-size: 10.5px;
  line-height: 18px;
}
.solo-env__badge.solo-env__chip--project {
  background: rgb(var(--c-accent) / 0.12);
  color: rgb(var(--c-accent-2));
}
.solo-env__badge.solo-env__chip--credential {
  background: rgb(var(--c-warn) / 0.14);
  color: rgb(var(--c-warn));
}
.solo-env__badge.solo-env__chip--agent {
  background: rgb(var(--c-info) / 0.14);
  color: rgb(var(--c-info));
}
.solo-env__badge.solo-env__chip--platform,
.solo-env__badge.solo-env__chip--system {
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt3));
}
</style>
