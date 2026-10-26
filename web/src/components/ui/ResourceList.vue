<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '@/components/ui/Icon.vue'
export type ResourceListItem = { id: string; label: string; count?: number; icon?: string; locked?: boolean; lockTitle?: string }
const props = defineProps<{ nodes: ResourceListItem[]; pinned?: ResourceListItem[]; activeKey?: string; title: string; total?: number; searchPlaceholder?: string; emptyText?: string; noMatchText?: string; loading?: boolean }>()
const emit = defineEmits<{ select: [id: string]; contextmenu: [event: MouseEvent, id: string] }>()
const query = ref('')
// With pinned items above, "nothing created yet" copy would contradict them; while searching, use no-match copy.
const emptyMessage = computed(() => query.value.trim() ? (props.noMatchText ?? (props.pinned?.length ? '' : props.emptyText)) : (props.pinned?.length ? '' : props.emptyText))
const filtered = computed(() => props.nodes.filter((node) => node.label.toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase())))
</script>
<template>
  <div class="flex min-h-0 flex-1 flex-col" data-testid="resource-list">
    <div class="flex min-h-10 shrink-0 items-center gap-2 border-b border-line px-3">
      <span class="min-w-0 flex-1 truncate text-[12px] font-semibold text-txt2" data-testid="resource-list-title" :title="title">{{ title }}</span>
      <span v-if="total !== undefined" class="text-[11px] tabular-nums text-txt3">{{ total }}</span>
      <slot name="actions" />
    </div>
    <div class="shrink-0 p-2"><input v-model="query" class="input w-full text-xs" type="search" :aria-label="searchPlaceholder || title" :placeholder="searchPlaceholder" /></div>
    <div class="scroll-area min-h-0 flex-1 overflow-auto p-1.5" :aria-busy="loading">
      <div v-if="pinned?.length" class="mb-1 border-b border-line pb-1" data-testid="resource-list-pinned">
        <button v-for="node in pinned" :key="node.id" :data-resource-key="node.id" type="button" class="flex min-h-9 w-full items-center gap-2 rounded px-2 text-left text-[12px] hover:bg-overlay" :class="activeKey === node.id ? 'bg-accent-dim text-accent-2' : 'text-txt2'" :aria-current="activeKey === node.id ? 'true' : undefined" :title="node.label" @click="emit('select', node.id)">
          <Icon v-if="node.icon" :name="node.icon" :size="14" class="shrink-0" /><span class="min-w-0 flex-1 truncate">{{ node.label }}</span>
        </button>
      </div>
      <button v-for="node in filtered" :key="node.id" :data-resource-key="node.id" type="button" class="flex min-h-9 w-full items-center gap-2 rounded px-2 text-left text-[12px] hover:bg-overlay" :class="activeKey === node.id ? 'bg-accent-dim text-accent-2' : 'text-txt2'" :aria-current="activeKey === node.id ? 'true' : undefined" :title="node.label" @click="emit('select', node.id)" @contextmenu="emit('contextmenu', $event, node.id)">
        <Icon v-if="node.icon" :name="node.icon" :size="14" class="shrink-0" /><span class="min-w-0 flex-1 truncate">{{ node.label }}</span><span v-if="node.locked" class="shrink-0 text-txt3" data-testid="resource-lock" :title="node.lockTitle"><Icon name="lock" :size="12" /></span><span v-if="node.count !== undefined" class="text-[11px] tabular-nums text-txt3">{{ node.count }}</span>
      </button>
      <p v-if="!loading && !filtered.length && emptyMessage" class="px-2 py-4 text-center text-xs text-txt3" data-testid="resource-list-empty">{{ emptyMessage }}</p>
    </div>
  </div>
</template>
