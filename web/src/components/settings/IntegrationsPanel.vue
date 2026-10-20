<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import { BUILTIN_MCPS, type McpServer } from '@/data/mcp'

const { t } = useI18n()
const selected = ref<McpServer | null>(null)
const isProjectScope = computed(() => selected.value?.scope === 'project')
const isAgentScope = computed(() => selected.value?.scope === 'agent')
const isRunScope = computed(() => selected.value?.scope === 'run')

function openDetail(m: McpServer) {
  selected.value = m
}

function backToCatalog() {
  selected.value = null
}

function scopeLabelKey(scope: McpServer['scope']): string {
  if (scope === 'project') return 'mcp.integrations.scopeProject'
  if (scope === 'agent') return 'mcp.integrations.scopeAgent'
  return 'mcp.integrations.scopeRun'
}

function availabilityBadgeKey(scope: McpServer['scope']): string {
  if (scope === 'project') return 'mcp.integrations.seenInPmLeader'
  if (scope === 'agent') return 'mcp.integrations.seenInAgentStudio'
  return 'mcp.integrations.alwaysAvailable'
}
</script>

<template>
  <section data-testid="integrations-panel">
    <header class="mb-8">
      <button
        v-if="selected"
        type="button"
        class="mb-4 inline-flex min-h-11 items-center gap-1 text-[13px] text-txt3 hover:text-txt"
        data-testid="integrations-panel-back"
        @click="backToCatalog"
      >
        <Icon name="chevron-left" :size="16" />
        {{ t('mcp.integrations.backToCatalog') }}
      </button>
      <h1 class="m-0 text-2xl font-semibold tracking-tight text-txt">{{ selected ? selected.name : t('mcp.integrations.title') }}</h1>
      <p class="mb-0 mt-2 text-[13px] leading-5 text-txt3">{{ selected ? t(selected.overviewKey || selected.descKey) : t('mcp.integrations.noConfigNeeded') }}</p>
    </header>

    <div data-testid="integrations-panel-content">
      <div v-if="!selected" data-testid="integrations-panel-catalog">
        <SettingsSection :title="t('mcp.integrations.sectionTitle')">
          <button
            v-for="m in BUILTIN_MCPS"
            :key="m.id"
            type="button"
            class="integration-catalog-row block w-full border-b border-line/60 text-left transition-colors last:border-b-0 hover:bg-elevated/40"
            data-testid="integrations-mcp-card"
            @click="openDetail(m)"
          >
            <SettingsRow :label="m.name" :description="t(m.descKey)" inline>
              <div class="flex items-center gap-3 text-txt3">
                <span class="text-[13px] tabular-nums" :aria-label="t('mcp.integrations.toolCount', { n: m.tools.length })">{{ m.tools.length }}</span>
                <Icon name="chevron-right" :size="16" />
              </div>
              <template #meta>
                <span>{{ t(scopeLabelKey(m.scope)) }}</span>
                <span aria-hidden="true">·</span>
                <span>{{ t(availabilityBadgeKey(m.scope)) }}</span>
              </template>
            </SettingsRow>
          </button>
        </SettingsSection>
      </div>

      <div v-else data-testid="integrations-panel-detail">
        <div class="mb-7 text-[13px] leading-5 text-txt3">
          <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
            <code class="text-xs">{{ selected.id }}</code>
            <span>{{ t(scopeLabelKey(selected.scope)) }}</span>
            <span>{{ t(availabilityBadgeKey(selected.scope)) }}</span>
          </div>
          <template v-if="isProjectScope">
            <p class="mb-0 mt-3">{{ t('mcp.integrations.notRunScoped') }} · {{ t('mcp.integrations.noConfigHere') }}</p>
            <p class="mb-0 mt-1">{{ t('mcp.integrations.entryPathLabel') }}<span class="text-txt2">{{ t('mcp.integrations.entryPath') }}</span></p>
          </template>
          <template v-else-if="isAgentScope">
            <p class="mb-0 mt-3">{{ t('mcp.integrations.notRunScoped') }} · {{ t('mcp.integrations.addViaAgentStudio') }}</p>
            <p class="mb-0 mt-1">{{ t('mcp.integrations.entryPathLabel') }}<span class="text-txt2">{{ t('mcp.integrations.entryPathAgent') }}</span></p>
          </template>
          <p v-else-if="isRunScope" class="mb-0 mt-3">{{ t('mcp.integrations.noConfigNeeded') }}</p>
        </div>

        <SettingsSection :title="t('mcp.integrations.toolsTitle') + ' · ' + selected.tools.length">
          <SettingsRow v-for="tool in selected.tools" :key="tool.name" :label="tool.name" :description="t(tool.descKey)" inline data-testid="integration-tool-row">
            <span class="font-mono text-[11px] text-txt3">{{ t(`mcp.integrations.io.${tool.io}`) }}</span>
            <template #details>
              <code class="block break-all font-mono text-[11px] leading-5 text-txt2">{{ t(tool.signatureKey) }}</code>
            </template>
          </SettingsRow>
          <template v-if="selected.conventionKey" #footer>
            <p class="mb-0 mt-3 flex items-start gap-2 text-xs leading-5 text-warn">
              <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
              {{ t(selected.conventionKey) }}
            </p>
          </template>
        </SettingsSection>
      </div>
    </div>
  </section>
</template>
