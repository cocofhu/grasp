<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import CodeEditor from '@/components/ui/CodeEditor.vue'
import McpConfigHelpModal from '@/components/agent/McpConfigHelpModal.vue'
import type { MCPServer } from '@/lib/api/api'
import {
  AGENT_PLATFORM_MCPS,
  ARTIFACT_STORE,
  DEFAULT_CONFIG_ROOT,
  apiMcpToDraft,
  draftMcpToApi,
  isPlatformPresetName,
  platformPresetKind,
  type AgentStudioDraft,
} from '@/lib/agent/agentStudioDraft'

const props = defineProps<{ draft: AgentStudioDraft }>()
const emit = defineEmits<{ toast: [msg: string] }>()

const { t } = useI18n()

const mcpRaw = ref(false)
const mcpRawText = ref('')
const rawError = ref('')
const mcpHelpOpen = ref(false)

const hasArtifactStore = computed(() => !!props.draft.mcp.some((m) => m.name.trim() === ARTIFACT_STORE))

function hasAgentPlatformMcp(name: string) {
  return !!props.draft.mcp.some((m) => m.name.trim() === name)
}

function addMcp() {
  props.draft.mcp.push({ name: '', transport: 'url', url: '', headers: [], command: '', args: '', env: [] })
}

function removeMcp(i: number) {
  props.draft.mcp.splice(i, 1)
}

function addArtifactStore() {
  if (hasArtifactStore.value) return
  props.draft.mcp.unshift({
    name: ARTIFACT_STORE,
    transport: 'url',
    url: '${GRASP_ARTIFACT_URL}',
    headers: [{ k: 'Authorization', v: 'Bearer ${GRASP_ARTIFACT_TOKEN}' }],
    command: '',
    args: '',
    env: [],
  })
}

function addAgentPlatformMcp(name: (typeof AGENT_PLATFORM_MCPS)[number]['name']) {
  const spec = AGENT_PLATFORM_MCPS.find((m) => m.name === name)
  if (!spec) return
  if (hasAgentPlatformMcp(name)) {
    emit('toast', t('pages.agentStudio.mcp.agentMcpAlreadyExists', { name }))
    return
  }
  props.draft.mcp.push({
    name: spec.name,
    transport: 'url',
    url: spec.url,
    headers: [{ k: 'Authorization', v: `Bearer ${spec.token}` }],
    command: '',
    args: '',
    env: [],
  })
}

function toggleMcpRaw() {
  mcpRaw.value = !mcpRaw.value
  rawError.value = ''
  if (mcpRaw.value) mcpRawText.value = JSON.stringify(props.draft.mcp.map(draftMcpToApi), null, 2)
}

function onMcpRaw(text: string) {
  mcpRawText.value = text
  try {
    const arr = JSON.parse(text)
    if (!Array.isArray(arr)) throw new Error(t('pages.agentStudio.dialogs.jsonArray'))
    props.draft.mcp = (arr as MCPServer[]).map(apiMcpToDraft)
    rawError.value = ''
  } catch (e: any) {
    rawError.value = t('pages.agentStudio.mcp.parseError') + (e?.message || e)
  }
}

watch(
  () => props.draft.name,
  () => {
    mcpRaw.value = false
    rawError.value = ''
  },
)
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="toolbar-inline-row flex items-center gap-2 border-b border-line px-4">
      <button class="rounded border border-line px-2 py-1 text-[11px] text-txt2 hover:border-line-strong" @click="toggleMcpRaw">{{ mcpRaw ? t('pages.agentStudio.mcp.formEdit') : t('pages.agentStudio.mcp.rawJson') }}</button>
      <AppButton
        v-if="!mcpRaw"
        type="button"
        size="sm"
        variant="outline"
        icon="help"
        data-test="mcp-help-link"
        @click="mcpHelpOpen = true"
      >
        {{ t('pages.agentStudio.mcpHelp.link') }}
      </AppButton>
      <span class="flex-1" />
    </div>

    <div v-if="mcpRaw" class="flex min-h-0 flex-1 flex-col">
      <div v-if="rawError" class="border-b border-err/30 bg-err/10 px-4 py-1.5 text-[11px] text-err">{{ rawError }}</div>
      <div class="min-h-0 flex-1"><CodeEditor :model-value="mcpRawText" language="json" @update:model-value="onMcpRaw" /></div>
    </div>

    <div v-else class="scroll-area flex-1 space-y-3 overflow-y-auto p-4">
      <div class="rounded-lg flex flex-wrap items-center gap-1.5 border border-line bg-elevated p-2.5" data-test="mcp-ops-bar">
        <span class="mr-1 text-[11px] text-txt3">{{ t('pages.agentStudio.mcp.quickAddLabel') }}</span>
        <button
          v-if="!hasArtifactStore"
          class="rounded border border-accent/40 px-2 py-1 text-[11px] text-accent-2 hover:bg-accent-dim"
          type="button"
          data-test="mcp-add-artifact"
          @click="addArtifactStore"
        >{{ t('pages.agentStudio.mcp.addArtifactStore') }}</button>
        <button
          class="rounded border border-accent/40 px-2 py-1 text-[11px] text-accent-2 hover:bg-accent-dim"
          type="button"
          data-test="mcp-add-memory"
          @click="addAgentPlatformMcp('memory-store')"
        >{{ t('pages.agentStudio.mcp.addMemoryStore') }}</button>
        <button
          class="rounded border border-accent/40 px-2 py-1 text-[11px] text-accent-2 hover:bg-accent-dim"
          type="button"
          data-test="mcp-add-context"
          @click="addAgentPlatformMcp('context-store')"
        >{{ t('pages.agentStudio.mcp.addContextStore') }}</button>
        <button
          class="rounded border border-accent/40 px-2 py-1 text-[11px] text-accent-2 hover:bg-accent-dim"
          type="button"
          data-test="mcp-add-scheduler"
          @click="addAgentPlatformMcp('task-scheduler')"
        >{{ t('pages.agentStudio.mcp.addTaskScheduler') }}</button>
      </div>
      <div
        v-for="(m, i) in draft.mcp"
        :key="i"
        class="rounded-md border bg-base p-3"
        :class="isPlatformPresetName(m.name) ? 'border-ok/30' : 'border-line'"
        data-test="mcp-card"
        :data-mcp-name="m.name.trim()"
      >
        <div class="mb-2 flex items-center gap-2">
          <div v-if="isPlatformPresetName(m.name)" class="min-w-0 flex-1">
            <div class="text-[12px] font-medium text-txt" data-test="mcp-display-name">{{ t(`pages.agentStudio.mcp.displayName.${platformPresetKind(m.name)}`) }}</div>
            <div class="font-mono text-[10.5px] text-txt3" data-test="mcp-preset-key">{{ m.name.trim() }}</div>
          </div>
          <input
            v-else
            v-model="m.name"
            :placeholder="t('pages.agentStudio.mcp.serviceName')"
            class="flex-1 rounded border border-line bg-surface px-2 py-1 text-[12px] text-txt outline-none focus:border-accent"
            data-test="mcp-custom-name"
          />
          <select v-model="m.transport" class="rounded border border-line bg-surface px-2 py-1 text-[12px] text-txt2 outline-none">
            <option value="url">HTTP (url)</option>
            <option value="command">{{ t('pages.agentStudio.mcp.transportCommand') }}</option>
          </select>
          <button class="text-txt3 hover:text-err" :title="t('pages.agentStudio.mcp.remove')" data-test="mcp-remove" @click="removeMcp(i)"><Icon name="close" :size="14" /></button>
        </div>

        <template v-if="m.transport === 'url'">
          <input v-model="m.url" placeholder="https://mcp.example.com/sse" class="mb-2 w-full rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt outline-none focus:border-accent" />
          <div class="text-[11px] text-txt3">{{ t('pages.agentStudio.mcp.headers') }}</div>
          <div v-for="(h, hi) in m.headers" :key="hi" class="mt-1 flex items-center gap-1.5">
            <input v-model="h.k" placeholder="Authorization" class="w-1/3 rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt2 outline-none" />
            <input v-model="h.v" placeholder="Bearer …" class="flex-1 rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt2 outline-none" />
            <button class="text-txt3 hover:text-err" @click="m.headers.splice(hi, 1)"><Icon name="close" :size="12" /></button>
          </div>
          <button class="mt-1.5 text-[11px] text-accent-2 hover:underline" @click="m.headers.push({ k: '', v: '' })">{{ t('pages.agentStudio.mcp.addHeader') }}</button>
        </template>

        <template v-else>
          <input v-model="m.command" placeholder="npx" class="mb-2 w-full rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt outline-none focus:border-accent" />
          <div class="text-[11px] text-txt3">{{ t('pages.agentStudio.mcp.args') }}</div>
          <textarea v-model="m.args" rows="2" placeholder="-y&#10;@upstash/context7-mcp" class="mt-1 w-full resize-y rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt2 outline-none" />
          <div class="mt-2 text-[11px] text-txt3">{{ t('pages.agentStudio.mcp.env') }}</div>
          <div v-for="(e, ei) in m.env" :key="ei" class="mt-1 flex items-center gap-1.5">
            <input v-model="e.k" placeholder="KEY" class="w-1/3 rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt2 outline-none" />
            <input v-model="e.v" placeholder="value" class="flex-1 rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt2 outline-none" />
            <button class="text-txt3 hover:text-err" @click="m.env.splice(ei, 1)"><Icon name="close" :size="12" /></button>
          </div>
          <button class="mt-1.5 text-[11px] text-accent-2 hover:underline" @click="m.env.push({ k: '', v: '' })">{{ t('pages.agentStudio.mcp.addEnv') }}</button>
        </template>

        <div
          v-if="isPlatformPresetName(m.name)"
          class="rounded-lg mt-2.5 border border-dashed border-ok/35 bg-ok/5 p-2 text-[10.5px] leading-5 text-txt2"
          data-test="mcp-scope-note"
        >
          {{ t(`pages.agentStudio.mcp.scopeNote.${platformPresetKind(m.name)}`) }}
        </div>
      </div>
      <AppButton size="sm" variant="outline" icon="plus" @click="addMcp">{{ t('pages.agentStudio.mcp.addService') }}</AppButton>
    </div>

    <McpConfigHelpModal
      :open="mcpHelpOpen"
      :config-root="draft.layout?.configRoot || DEFAULT_CONFIG_ROOT"
      @close="mcpHelpOpen = false"
    />
  </div>
</template>
