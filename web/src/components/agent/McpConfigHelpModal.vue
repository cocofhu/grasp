<script setup lang="ts">
import { nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'

const props = withDefaults(
  defineProps<{
    open: boolean
    configRoot?: string
  }>(),
  { configRoot: '' },
)

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

let lastTrigger: HTMLElement | null = null

watch(
  () => props.open,
  (open, wasOpen) => {
    if (open && !wasOpen) {
      lastTrigger = document.activeElement instanceof HTMLElement ? document.activeElement : null
    }
    if (!open && wasOpen) {
      nextTick(() => lastTrigger?.focus())
    }
  },
)

function onClose() {
  emit('close')
}
</script>

<template>
  <AppModal
    :open="open"
    :title="t('pages.agentStudio.mcpHelp.title')"
    :width="640"
    close-on-esc
    @close="onClose"
  >
    <p class="rounded-md mb-3 border border-line bg-base px-3 py-2.5 text-[12px] leading-[1.7] text-txt2" data-test="mcp-help-hint">
      {{ t('pages.agentStudio.mcp.hint', { configRoot: configRoot || '' }) }}
    </p>

    <section data-test="mcp-help-run" class="scroll-mt-2">
      <h3 class="mb-2 mt-0 flex items-center gap-1.5 text-[13px] font-semibold text-txt">
        {{ t('pages.agentStudio.mcp.runVarsTitle') }}
        <span class="rounded-md border border-info/35 px-1.5 py-px text-[10px] font-medium text-info">{{ t('pages.agentStudio.mcp.runScopeTag') }}</span>
      </h3>
      <div class="grid gap-2 font-mono text-[12px] text-txt3">
        <div><code class="text-accent-2">${GRASP_ARTIFACT_URL}</code> — {{ t('pages.agentStudio.mcp.artifactUrl') }}</div>
        <div><code class="text-accent-2">${GRASP_ARTIFACT_TOKEN}</code> — {{ t('pages.agentStudio.mcp.artifactToken') }}</div>
        <div><code class="text-accent-2">${GRASP_RUN_ID}</code> · <code class="text-accent-2">${GRASP_NODE_ID}</code></div>
        <div><code class="text-accent-2">${vars.&lt;name&gt;}</code> — {{ t('pages.agentStudio.mcp.globalVar') }}</div>
      </div>
    </section>

    <template #footer>
      <AppButton variant="primary" data-test="mcp-help-got-it" @click="onClose">
        {{ t('pages.agentStudio.mcpHelp.gotIt') }}
      </AppButton>
    </template>
  </AppModal>
</template>
