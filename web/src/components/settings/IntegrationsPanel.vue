<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import SettingsPageHeader from '@/components/settings/SettingsPageHeader.vue'
import { BUILTIN_MCPS, type McpServer } from '@/data/mcp'

const { t } = useI18n()
const selected = ref<McpServer | null>(null)

function openDetail(m: McpServer) {
  selected.value = m
}

function backToCatalog() {
  selected.value = null
}
</script>

<template>
  <section data-testid="integrations-panel">
    <div class="mb-6">
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
      <SettingsPageHeader
        icon="integration"
        :title="selected ? selected.name : t('mcp.integrations.title')"
        :description="selected ? t(selected.overviewKey || selected.descKey) : t('mcp.integrations.noConfigNeeded')"
      />
    </div>

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
                <span>{{ t('mcp.integrations.scopeRun') }}</span>
                <span aria-hidden="true">·</span>
                <span>{{ t('mcp.integrations.alwaysAvailable') }}</span>
              </template>
            </SettingsRow>
          </button>
        </SettingsSection>
      </div>

      <div v-else data-testid="integrations-panel-detail">
        <div class="mb-7 text-[13px] leading-5 text-txt3">
          <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
            <code class="text-xs">{{ selected.id }}</code>
            <span>{{ t('mcp.integrations.scopeRun') }}</span>
            <span>{{ t('mcp.integrations.alwaysAvailable') }}</span>
          </div>
          <p class="mb-0 mt-3">{{ t('mcp.integrations.noConfigNeeded') }}</p>
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

<style scoped>
.integration-catalog-row { width: calc(100% + 40px); margin-inline: -20px; padding-inline: 20px; }
@media (max-width: 767px) {
  .integration-catalog-row { width: calc(100% + 32px); margin-inline: -16px; padding-inline: 16px; }
}
</style>
