<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import { buildOutputSourceOptions, labelForOutputTemplate } from '@/lib/run/outputSourceOptions'
import { useToast } from '@/lib/composables/useToast'
import type { WFEdge, WFNode } from '@/lib/shared/types'

const props = defineProps<{
  node: WFNode
  allNodes: WFNode[]
  edges: WFEdge[]
}>()

const { t } = useI18n()
const toast = useToast()

if (!Array.isArray(props.node.config.results)) {
  props.node.config.results = []
}

const selected = computed<string[]>({
  get: () => (props.node.config.results as string[]) || [],
  set: (v) => {
    props.node.config.results = v
  },
})

const availableOptions = computed(() =>
  buildOutputSourceOptions(props.allNodes, props.edges, props.node.id, t),
)

const selectedSet = computed(() => new Set(selected.value))

const dragId = ref<string | null>(null)
const dragOverId = ref<string | null>(null)

function labelFor(template: string): string {
  const known = labelForOutputTemplate(template, props.allNodes, props.edges, props.node.id, t)
  const inOpts = availableOptions.value.some((o) => o.value === template)
  if (inOpts) return known
  return `${known} (${t('pages.workflowEditor.inspector.outputSources.customSource')})`
}

function remove(template: string) {
  selected.value = selected.value.filter((x) => x !== template)
}

function add(template: string) {
  if (selectedSet.value.has(template)) {
    toast.warn(t('pages.workflowEditor.inspector.outputSources.duplicateHint'))
    return
  }
  selected.value = [...selected.value, template]
  toast.success(t('pages.workflowEditor.inspector.outputSources.addedToast'))
}

function onDragStart(template: string, e: DragEvent) {
  dragId.value = template
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}

function onDrop(target: string) {
  const from = dragId.value
  dragId.value = null
  dragOverId.value = null
  if (!from || from === target) return
  const list = [...selected.value]
  const fromIdx = list.indexOf(from)
  const toIdx = list.indexOf(target)
  if (fromIdx < 0 || toIdx < 0) return
  list.splice(fromIdx, 1)
  list.splice(toIdx, 0, from)
  selected.value = list
  toast.success(t('pages.workflowEditor.inspector.outputSources.reorderedToast'))
}
</script>

<template>
  <div>
    <div class="rounded-lg border border-line bg-base">
      <template v-if="selected.length">
        <div
          v-for="(template, i) in selected"
          :key="template"
          class="flex cursor-grab items-center gap-2 border-b border-line px-2.5 py-2 last:border-b-0 hover:bg-elevated"
          :class="dragId === template ? 'opacity-45 bg-accent-dim' : dragOverId === template ? 'ring-1 ring-inset ring-accent-2' : ''"
          draggable="true"
          @dragstart="onDragStart(template, $event)"
          @dragend="dragId = null; dragOverId = null"
          @dragover.prevent="dragOverId = template"
          @dragleave="dragOverId = null"
          @drop.prevent="onDrop(template)"
        >
          <span class="cursor-grab text-[12px] text-txt3">⠿</span>
          <span class="inline-flex w-[22px] shrink-0 items-center justify-center bg-accent-dim py-0.5 text-[10px] tabular-nums text-accent-2">{{ i + 1 }}</span>
          <AppSwitch
            :model-value="true"
            :aria-label="labelFor(template)"
            @update:model-value="(on) => { if (!on) remove(template) }"
          />
          <div class="min-w-0 flex-1">
            <div class="truncate text-[12px] text-txt">{{ labelFor(template) }}</div>
            <div class="truncate font-mono text-[10px] text-txt3">{{ template }}</div>
          </div>
        </div>
      </template>
      <div v-else class="px-3 py-6 text-center text-[12px] text-txt3">
        {{ t('pages.workflowEditor.inspector.outputSources.emptySelected') }}
      </div>
    </div>

    <div class="mt-3.5">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('pages.workflowEditor.inspector.outputSources.availableSection') }}
      </div>
      <div
        v-if="!availableOptions.length"
        data-testid="output-sources-empty-available"
        class="rounded-lg border border-dashed border-line px-3 py-4 text-[12px] leading-relaxed text-txt3"
      >
        {{ t('pages.workflowEditor.inspector.outputSources.emptyAvailable') }}
      </div>
      <button
        v-for="opt in availableOptions"
        :key="opt.value"
        type="button"
        class="rounded-md mb-1 flex w-full items-center gap-2 border border-line bg-base px-2.5 py-1.5 text-left text-[12px] transition"
        :class="selectedSet.has(opt.value) ? 'cursor-not-allowed opacity-50' : 'hover:border-line-strong hover:bg-elevated'"
        :disabled="selectedSet.has(opt.value)"
        @click="add(opt.value)"
      >
        <span>{{ selectedSet.has(opt.value) ? '✓' : '+' }}</span>
        <span class="flex-1 truncate">{{ opt.label }}</span>
        <span v-if="selectedSet.has(opt.value)" class="text-[10px] text-warn">
          {{ t('pages.workflowEditor.inspector.outputSources.alreadyAdded') }}
        </span>
      </button>
    </div>
  </div>
</template>
