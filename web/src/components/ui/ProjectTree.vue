<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppSkeleton from './AppSkeleton.vue'
import EmptyState from './EmptyState.vue'
import Icon from './Icon.vue'
import TruncatedTextTooltip from './TruncatedTextTooltip.vue'
import {
  childKey,
  filterProjectTree,
  flattenProjectTree,
  loadExpandedProjects,
  parseTreeKey,
  projectKey,
  saveExpandedProjects,
  type ProjectTreeNode,
  type ProjectTreeRow,
} from './projectTree'

const props = defineProps<{
  nodes: ProjectTreeNode[]
  activeKey: string
  title: string
  total?: number
  searchPlaceholder?: string
  loading?: boolean
  emptyText?: string
  storageKey: string
}>()

const emit = defineEmits<{
  (e: 'select', key: string): void
  (e: 'contextmenu', ev: MouseEvent, key: string): void
}>()

const { t } = useI18n()

const query = ref('')
const treeEl = ref<HTMLElement | null>(null)
const focusedKey = ref('')

const stored = loadExpandedProjects(props.storageKey)
const expanded = ref<Set<string>>(stored ?? new Set())
/** Projects the user collapsed while a search forced them open; reset per query. */
const searchCollapsed = ref<Set<string>>(new Set())
/** Whether a valid activeKey has been seen; the first one may arrive after async loads. */
let sawActiveKey = false

watch(
  () => props.activeKey,
  (key) => {
    const parsed = parseTreeKey(key)
    if (!parsed) return
    const isFirst = !sawActiveKey
    sawActiveKey = true
    if (parsed.childId || (isFirst && !stored)) expandProject(parsed.projectId)
  },
  { immediate: true },
)

watch(query, () => {
  searchCollapsed.value = new Set()
})

const filtered = computed(() => filterProjectTree(props.nodes, query.value))
const searching = computed(() => query.value.trim() !== '')

function isExpanded(id: string): boolean {
  if (searching.value && filtered.value.forcedOpen.has(id)) return !searchCollapsed.value.has(id)
  return expanded.value.has(id)
}

const rows = computed<ProjectTreeRow[]>(() => flattenProjectTree(filtered.value.nodes, isExpanded))

const rowsByProject = computed(() => {
  const groups: { project: Extract<ProjectTreeRow, { kind: 'project' }>; children: Extract<ProjectTreeRow, { kind: 'child' }>[] }[] = []
  for (const row of rows.value) {
    if (row.kind === 'project') groups.push({ project: row, children: [] })
    else groups[groups.length - 1]?.children.push(row)
  }
  return groups
})

const tabStopKey = computed(() => {
  const keys = rows.value.map((r) => r.key)
  if (focusedKey.value && keys.includes(focusedKey.value)) return focusedKey.value
  if (keys.includes(props.activeKey)) return props.activeKey
  return keys[0] ?? ''
})

function persist() {
  saveExpandedProjects(props.storageKey, expanded.value)
}

function expandProject(id: string) {
  if (expanded.value.has(id)) return
  const next = new Set(expanded.value)
  next.add(id)
  expanded.value = next
  persist()
}

function collapseProject(id: string) {
  if (searching.value && filtered.value.forcedOpen.has(id)) {
    searchCollapsed.value = new Set(searchCollapsed.value).add(id)
    return
  }
  if (!expanded.value.has(id)) return
  const next = new Set(expanded.value)
  next.delete(id)
  expanded.value = next
  persist()
}

function reopenProject(id: string) {
  if (searching.value && filtered.value.forcedOpen.has(id)) {
    const next = new Set(searchCollapsed.value)
    next.delete(id)
    searchCollapsed.value = next
    return
  }
  expandProject(id)
}

function toggleProject(id: string) {
  if (isExpanded(id)) collapseProject(id)
  else reopenProject(id)
}

function selectProject(id: string) {
  reopenProject(id)
  focusedKey.value = projectKey(id)
  emit('select', projectKey(id))
}

function selectChild(pid: string, cid: string) {
  const key = childKey(pid, cid)
  focusedKey.value = key
  emit('select', key)
}

function focusRow(key: string) {
  focusedKey.value = key
  void nextTick(() => {
    const el = treeEl.value?.querySelector<HTMLElement>(`[data-tree-key="${CSS.escape(key)}"]`)
    el?.focus()
    el?.scrollIntoView?.({ block: 'nearest' })
  })
}

function onTreeKeydown(e: KeyboardEvent) {
  const list = rows.value
  if (!list.length) return
  const idx = Math.max(0, list.findIndex((r) => r.key === tabStopKey.value))
  const row = list[idx]
  switch (e.key) {
    case 'ArrowDown':
      focusRow(list[Math.min(list.length - 1, idx + 1)].key)
      break
    case 'ArrowUp':
      focusRow(list[Math.max(0, idx - 1)].key)
      break
    case 'Home':
      focusRow(list[0].key)
      break
    case 'End':
      focusRow(list[list.length - 1].key)
      break
    case 'ArrowRight':
      if (row.kind !== 'project' || !row.hasChildren) return
      if (!row.expanded) reopenProject(row.node.id)
      else focusRow(list[idx + 1].key)
      break
    case 'ArrowLeft':
      if (row.kind === 'project') {
        if (!row.expanded) return
        collapseProject(row.node.id)
      } else {
        focusRow(projectKey(row.projectId))
      }
      break
    case 'Enter':
    case ' ':
      if (row.kind === 'project') selectProject(row.node.id)
      else selectChild(row.projectId, row.child.id)
      break
    default:
      return
  }
  e.preventDefault()
}

function onSearchKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && query.value) {
    query.value = ''
    e.stopPropagation()
    return
  }
  if (e.key === 'ArrowDown' && rows.value.length) {
    e.preventDefault()
    focusRow(rows.value[0].key)
  }
}

function rowClass(key: string) {
  const active = key === props.activeKey
  return active
    ? 'bg-accent-dim text-txt before:absolute before:inset-y-1.5 before:left-0 before:w-[2px] before:rounded-full before:bg-accent'
    : 'text-txt2 hover:bg-elevated hover:text-txt'
}

function badgeClass(key: string) {
  return key === props.activeKey
    ? 'border-accent/30 bg-base text-accent-2'
    : 'border-line bg-base text-txt3'
}
</script>

<template>
  <div class="flex h-full min-h-0 w-full min-w-0 flex-col" data-testid="project-tree">
    <div class="flex h-10 shrink-0 items-center gap-1.5 border-b border-line pl-3 pr-1.5">
      <span class="min-w-0 truncate text-[11px] font-semibold uppercase tracking-wider text-txt3">{{ title }}</span>
      <span
        v-if="total !== undefined"
        data-testid="project-tree-total"
        class="inline-flex h-4 min-w-[18px] shrink-0 items-center justify-center rounded-md border border-line bg-base px-1 text-[10px] font-semibold tabular-nums text-txt3"
      >{{ total }}</span>
      <span class="flex-1" />
      <slot name="actions" />
    </div>

    <div class="shrink-0 px-2 pb-1 pt-2">
      <label class="relative flex items-center">
        <Icon name="search" :size="13" class="pointer-events-none absolute left-2.5 text-txt3" />
        <input
          v-model="query"
          type="search"
          data-testid="project-tree-search"
          class="h-10 w-full rounded-md border border-line bg-surface pl-8 pr-7 text-[12.5px] text-txt placeholder:text-txt3 transition-colors focus:border-accent/60 focus:outline-none md:h-8 [&::-webkit-search-cancel-button]:hidden"
          :placeholder="searchPlaceholder || t('common.projectTree.searchPlaceholder')"
          :aria-label="searchPlaceholder || t('common.projectTree.searchPlaceholder')"
          @keydown="onSearchKeydown"
        />
        <button
          v-if="query"
          type="button"
          data-testid="project-tree-clear"
          class="absolute right-1.5 flex h-5 w-5 items-center justify-center rounded text-txt3 hover:bg-elevated hover:text-txt"
          :aria-label="t('common.projectTree.clearSearch')"
          @click="query = ''"
        >
          <Icon name="close" :size="11" />
        </button>
      </label>
    </div>

    <div class="scroll-area min-h-0 flex-1 overflow-y-auto px-1.5 pb-2 pt-1">
      <AppSkeleton v-if="loading && !nodes.length" list :rows="3" class="px-1" />
      <EmptyState
        v-else-if="!nodes.length"
        icon="folder"
        :title="emptyText || t('common.projectTree.empty')"
      />
      <div
        v-else-if="!rows.length"
        data-testid="project-tree-no-match"
        class="px-3 py-6 text-center text-xs text-txt3"
      >{{ t('common.projectTree.noMatch', { q: query.trim() }) }}</div>
      <div
        v-else
        ref="treeEl"
        role="tree"
        :aria-label="title"
        class="flex flex-col gap-1"
        @keydown="onTreeKeydown"
      >
        <div v-for="group in rowsByProject" :key="group.project.key" role="none">
          <div
            role="treeitem"
            :aria-level="1"
            :aria-selected="group.project.key === activeKey"
            :aria-expanded="group.project.hasChildren ? group.project.expanded : undefined"
            :tabindex="group.project.key === tabStopKey ? 0 : -1"
            :data-tree-key="group.project.key"
            data-tree-kind="project"
            class="relative flex h-10 cursor-pointer select-none items-center gap-1 rounded-md pl-1 pr-2 text-[12.5px] outline-none transition-colors duration-[var(--dur-ui)] focus-visible:ring-1 focus-visible:ring-accent/60 md:h-8"
            :class="rowClass(group.project.key)"
            @click="selectProject(group.project.node.id)"
            @focus="focusedKey = group.project.key"
            @contextmenu="emit('contextmenu', $event, group.project.key)"
          >
            <button
              v-if="group.project.hasChildren"
              type="button"
              tabindex="-1"
              data-tree-toggle
              class="flex h-6 w-6 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt md:h-5 md:w-5"
              :aria-label="group.project.expanded ? t('common.projectTree.collapse') : t('common.projectTree.expand')"
              @click.stop="toggleProject(group.project.node.id)"
            >
              <Icon
                name="chevron-right"
                :size="12"
                class="ui-fold-chevron"
                :class="group.project.expanded ? 'rotate-90' : ''"
              />
            </button>
            <span v-else class="h-6 w-6 shrink-0 md:h-5 md:w-5" aria-hidden="true" />
            <Icon
              :name="group.project.node.icon || 'folder'"
              :size="14"
              class="shrink-0"
              :class="group.project.key === activeKey ? 'text-accent-2' : 'text-warn'"
            />
            <TruncatedTextTooltip
              :text="group.project.node.label"
              :focusable="false"
              class="ml-1 min-w-0 flex-1 truncate font-semibold"
            />
            <span
              v-if="group.project.node.count !== undefined"
              data-tree-count
              class="ml-1 inline-flex h-4 min-w-[18px] shrink-0 items-center justify-center rounded-md border px-1 text-[10px] font-semibold tabular-nums"
              :class="badgeClass(group.project.key)"
            >{{ group.project.node.count }}</span>
          </div>

          <div v-if="group.children.length" role="group" class="mt-0.5 flex flex-col gap-px">
            <div
              v-for="row in group.children"
              :key="row.key"
              role="treeitem"
              :aria-level="2"
              :aria-selected="row.key === activeKey"
              :tabindex="row.key === tabStopKey ? 0 : -1"
              :data-tree-key="row.key"
              data-tree-kind="child"
              class="relative flex h-10 cursor-pointer select-none items-center gap-1.5 rounded-md pl-[33px] pr-2 text-[12.5px] outline-none transition-colors duration-[var(--dur-ui)] focus-visible:ring-1 focus-visible:ring-accent/60 md:h-8"
              :class="rowClass(row.key)"
              @click="selectChild(row.projectId, row.child.id)"
              @focus="focusedKey = row.key"
              @contextmenu="emit('contextmenu', $event, row.key)"
            >
              <Icon
                :name="row.child.icon || 'doc'"
                :size="13"
                class="shrink-0"
                :class="row.key === activeKey ? 'text-accent-2' : 'text-txt3'"
              />
              <TruncatedTextTooltip
                :text="row.child.label"
                :focusable="false"
                class="min-w-0 flex-1 truncate"
              />
              <span
                v-if="row.child.count !== undefined"
                data-tree-count
                class="ml-1 shrink-0 text-[10.5px] tabular-nums"
                :class="row.key === activeKey ? 'text-accent-2' : 'text-txt3'"
              >{{ row.child.count }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
