<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import CodeEditor from '@/components/ui/CodeEditor.vue'
import McpConfigHelpModal from '@/components/agent/McpConfigHelpModal.vue'
import type { MCPServer } from '@/lib/api/api'
import { MCP_PRESET_LABEL_KEYS, loadMcpPresets, presetDraft, type MCPPreset } from '@/lib/agent/mcpPresets'
import {
  ARTIFACT_STORE,
  DEFAULT_CONFIG_ROOT,
  apiMcpToDraft,
  draftMcpToApi,
  isPlatformPresetName,
  type AgentStudioDraft,
} from '@/lib/agent/agentStudioDraft'

const props = defineProps<{ draft: AgentStudioDraft }>()
const emit = defineEmits<{ toast: [msg: string] }>()

const { t } = useI18n()

const mcpRaw = ref(false)
const mcpRawText = ref('')
const rawError = ref('')
const mcpHelpOpen = ref(false)
const transportOptions = computed(() => [{ value: 'url', label: 'HTTP (url)' }, { value: 'command', label: t('pages.agentStudio.mcp.transportCommand') }])

const presets = ref<MCPPreset[]>([])
const presentNames = computed(() => new Set(props.draft.mcp.map((m) => m.name.trim())))
const quickAdd = computed(() => presets.value.filter((p) => !presentNames.value.has(p.server.name)))
const desktopOnlyNames = computed(() => new Set(presets.value.filter((p) => p.requiresDesktop).map((p) => p.server.name)))

onMounted(async () => {
  presets.value = await loadMcpPresets()
})

function addMcp() {
  props.draft.mcp.push({ name: '', transport: 'url', url: '', headers: [], command: '', args: '', env: [] })
}

function removeMcp(i: number) {
  props.draft.mcp.splice(i, 1)
}

function addPreset(p: MCPPreset) {
  if (presentNames.value.has(p.server.name)) return
  if (p.server.name === ARTIFACT_STORE) props.draft.mcp.unshift(presetDraft(p))
  else props.draft.mcp.push(presetDraft(p))
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
      <div class="flex flex-wrap items-center gap-1.5 border-b border-line/60 pb-3" data-test="mcp-ops-bar">
        <span class="mr-1 text-[11px] text-txt3">{{ t('pages.agentStudio.mcp.quickAddLabel') }}</span>
        <button
          v-for="p in quickAdd"
          :key="p.id"
          class="rounded border border-accent/40 px-2 py-1 text-[11px] text-accent-2 hover:bg-accent-dim"
          type="button"
          :title="p.requiresDesktop ? t('pages.agentStudio.mcp.requiresDesktop') : undefined"
          :data-test="`mcp-add-${p.id}`"
          @click="addPreset(p)"
        >{{ t(MCP_PRESET_LABEL_KEYS[p.id]) }}</button>
      </div>
      <div
        v-for="(m, i) in draft.mcp"
        :key="i"
        class="border-b py-4"
        :class="isPlatformPresetName(m.name) ? 'border-ok/30' : 'border-line'"
        data-test="mcp-card"
        :data-mcp-name="m.name.trim()"
      >
        <div class="mb-2 flex items-center gap-2">
          <div v-if="isPlatformPresetName(m.name)" class="min-w-0 flex-1">
            <div class="text-[12px] font-medium text-txt" data-test="mcp-display-name">{{ t('pages.agentStudio.mcp.displayName.artifact') }}</div>
            <div class="font-mono text-[10.5px] text-txt3" data-test="mcp-preset-key">{{ m.name.trim() }}</div>
          </div>
          <input
            v-else
            v-model="m.name"
            :placeholder="t('pages.agentStudio.mcp.serviceName')"
            class="min-w-0 flex-1 rounded border border-line bg-surface px-2 py-1 text-[12px] text-txt outline-none focus:border-accent"
            data-test="mcp-custom-name"
          />
          <AppSelect :model-value="m.transport" :options="transportOptions" :aria-label="`MCP ${m.name || i + 1}`" class="w-40 max-w-[50%] shrink-0" size="sm" searchable :data-testid="`mcp-transport-${i}`" @update:model-value="value => { if (value === 'url' || value === 'command') m.transport = value }" />
          <button class="text-txt3 hover:text-err" :title="t('pages.agentStudio.mcp.remove')" data-test="mcp-remove" @click="removeMcp(i)"><Icon name="close" :size="14" /></button>
        </div>

        <template v-if="m.transport === 'url'">
          <input v-model="m.url" placeholder="https://mcp.example.com/sse" class="mb-2 w-full rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt outline-none focus:border-accent" />
          <div class="text-[11px] text-txt3">{{ t('pages.agentStudio.mcp.headers') }}</div>
          <div v-for="(h, hi) in m.headers" :key="hi" class="mt-1 flex items-center gap-1.5">
            <input v-model="h.k" placeholder="Authorization" class="w-1/3 min-w-0 rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt2 outline-none" />
            <input v-model="h.v" placeholder="Bearer …" class="min-w-0 flex-1 rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt2 outline-none" />
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
            <input v-model="e.k" placeholder="KEY" class="w-1/3 min-w-0 rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt2 outline-none" />
            <input v-model="e.v" placeholder="value" class="min-w-0 flex-1 rounded border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt2 outline-none" />
            <button class="text-txt3 hover:text-err" @click="m.env.splice(ei, 1)"><Icon name="close" :size="12" /></button>
          </div>
          <button class="mt-1.5 text-[11px] text-accent-2 hover:underline" @click="m.env.push({ k: '', v: '' })">{{ t('pages.agentStudio.mcp.addEnv') }}</button>
        </template>

        <div
          v-if="isPlatformPresetName(m.name)"
          class="mt-3 border-l-2 border-ok/35 pl-3 text-[11px] leading-5 text-txt2"
          data-test="mcp-scope-note"
        >
          {{ t('pages.agentStudio.mcp.scopeNote.artifact') }}
        </div>
        <div
          v-else-if="desktopOnlyNames.has(m.name.trim())"
          class="mt-3 border-l-2 border-line-strong pl-3 text-[11px] leading-5 text-txt2"
          data-test="mcp-desktop-note"
        >
          {{ t('pages.agentStudio.mcp.scopeNote.desktopImage') }}
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
