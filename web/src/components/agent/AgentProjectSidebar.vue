<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import ProjectTree from '@/components/ui/ProjectTree.vue'
import { parseTreeKey, type ProjectTreeNode } from '@/components/ui/projectTree'

const props = defineProps<{
  nodes: ProjectTreeNode[]
  activeKey: string
  collapsed: boolean
  hideCreateTeam?: boolean
}>()

const emit = defineEmits<{
  (e: 'select', key: string): void
  (e: 'open-manage', agentName?: string): void
  (e: 'import'): void
  (e: 'create-agent', projectId?: string): void
  (e: 'create-team'): void
  (e: 'export-project', projectId: string): void
  (e: 'import-project', projectId: string): void
  (e: 'toggle-collapsed'): void
}>()

const { t } = useI18n()

type CtxState =
  | { kind: 'project'; x: number; y: number; projectId: string }
  | { kind: 'agent'; x: number; y: number; agentName: string }

const ctx = ref<CtxState | null>(null)

const total = computed(() => props.nodes.reduce((n, p) => n + (p.children?.length || 0), 0))

function closeCtx() {
  ctx.value = null
}

function onTreeContextMenu(e: MouseEvent, key: string) {
  const parsed = parseTreeKey(key)
  if (!parsed) return
  e.preventDefault()
  e.stopPropagation()
  ctx.value = parsed.childId
    ? { kind: 'agent', x: e.clientX, y: e.clientY, agentName: parsed.childId }
    : { kind: 'project', x: e.clientX, y: e.clientY, projectId: parsed.projectId }
}

function onCtxAction(action: 'createAgent' | 'export' | 'import' | 'renameViaManage') {
  const current = ctx.value
  if (!current) return
  closeCtx()
  if (current.kind === 'agent') {
    if (action === 'renameViaManage') emit('open-manage', current.agentName)
    return
  }
  const id = current.projectId
  if (action === 'createAgent') emit('create-agent', id)
  else if (action === 'export') emit('export-project', id)
  else if (action === 'import') emit('import-project', id)
}

const headerBtn =
  'flex h-[22px] w-[22px] shrink-0 items-center justify-center text-txt3 transition hover:bg-elevated hover:text-accent-2'
const ctxItem =
  'flex w-full items-center gap-2 px-3 py-1.5 text-left text-[12px] text-txt2 hover:bg-overlay hover:text-txt'
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-col border-r border-line" data-testid="agent-project-sidebar">
    <div
      v-if="collapsed"
      class="flex min-h-8 shrink-0 items-center justify-center border-b border-line px-[3px] py-1.5"
    >
      <button
        type="button"
        :class="headerBtn"
        :title="t('pages.agentStudio.agentList.expand')"
        @click="emit('toggle-collapsed')"
      >
        <Icon name="chevron-right" :size="14" />
      </button>
    </div>

    <ProjectTree
      v-else
      :nodes="nodes"
      :active-key="activeKey"
      :title="t('pages.agentStudio.tree.title')"
      :total="total"
      :search-placeholder="t('pages.agentStudio.tree.searchPlaceholder')"
      :empty-text="t('pages.agentStudio.tree.empty')"
      storage-key="agent-studio-tree"
      @select="emit('select', $event)"
      @contextmenu="onTreeContextMenu"
    >
      <template #actions>
        <button
          type="button"
          data-testid="agent-tree-manage"
          :class="headerBtn"
          :title="t('pages.agentStudio.tree.manageTitle')"
          :aria-label="t('pages.agentStudio.tree.manageTitle')"
          @click="emit('open-manage')"
        >
          <Icon name="user" :size="12" />
        </button>
        <button
          type="button"
          data-testid="agent-tree-import"
          :class="headerBtn"
          :title="t('pages.agentStudio.exportImport.import')"
          :aria-label="t('pages.agentStudio.exportImport.import')"
          @click="emit('import')"
        >
          <Icon name="input" :size="12" />
        </button>
        <button
          type="button"
          data-testid="agent-tree-create-agent"
          :class="headerBtn"
          :title="t('common.buttons.newAgent')"
          :aria-label="t('common.buttons.newAgent')"
          @click="emit('create-agent')"
        >
          <Icon name="plus" :size="12" />
        </button>
        <button
          v-if="!hideCreateTeam"
          type="button"
          data-testid="agent-tree-create-team"
          :class="headerBtn"
          :title="t('pages.agentStudio.tree.newTeam')"
          :aria-label="t('pages.agentStudio.tree.newTeam')"
          @click="emit('create-team')"
        >
          <Icon name="skills" :size="13" />
        </button>
        <button
          type="button"
          :class="headerBtn"
          :title="t('pages.agentStudio.agentList.collapse')"
          @click="emit('toggle-collapsed')"
        >
          <Icon name="chevron-right" :size="14" class="rotate-180" />
        </button>
      </template>
    </ProjectTree>

    <Teleport to="body">
      <div
        v-if="ctx"
        class="fixed inset-0 z-[9998]"
        data-tree-ctx-backdrop
        @click="closeCtx"
        @contextmenu.prevent="closeCtx"
      />
      <div
        v-if="ctx"
        class="rounded-lg fixed z-[9999] min-w-[180px] border border-line bg-elevated py-1 shadow-card"
        data-tree-ctx-menu
        :data-tree-ctx-kind="ctx.kind"
        :style="{ left: ctx.x + 'px', top: ctx.y + 'px' }"
        @click.stop
      >
        <template v-if="ctx.kind === 'project'">
          <button type="button" data-tree-ctx-action="createAgent" :class="ctxItem" @click="onCtxAction('createAgent')">
            <Icon name="plus" :size="13" class="text-txt3" />
            {{ t('pages.agentStudio.tree.createAgentHere') }}
          </button>
          <button type="button" data-tree-ctx-action="export" :class="ctxItem" @click="onCtxAction('export')">
            <Icon name="download" :size="13" class="text-txt3" />
            {{ t('pages.agentStudio.exportImport.export') }}
          </button>
          <button type="button" data-tree-ctx-action="import" :class="ctxItem" @click="onCtxAction('import')">
            <Icon name="input" :size="13" class="text-txt3" />
            {{ t('pages.agentStudio.exportImport.import') }}
          </button>
        </template>
        <template v-else>
          <button
            type="button"
            data-tree-ctx-action="renameViaManage"
            :class="ctxItem"
            @click="onCtxAction('renameViaManage')"
          >
            <Icon name="edit" :size="13" class="text-txt3" />
            {{ t('pages.agentStudio.tree.renameViaManage') }}
          </button>
        </template>
      </div>
    </Teleport>
  </div>
</template>
