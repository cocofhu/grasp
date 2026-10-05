<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Handle, Position } from '@vue-flow/core'
import Icon from '../../ui/Icon.vue'
import { useCanvasContext, type CanvasNodeData, type NodeMenuAction } from '../composables/canvasContext'

const props = defineProps<{
  id: string
  data: CanvasNodeData
  selected?: boolean
  width: number
}>()

const { t } = useI18n()
const ctx = useCanvasContext()

const isRun = computed(() => props.data.mode === 'run')
const labeledOutlets = computed(() => props.data.outlets.length > 1 || props.data.outlets.some((o) => o.id))
const plainOutlet = computed(() => !labeledOutlets.value && props.data.outlets.length === 1)

const stateClass = computed(() => {
  const c: string[] = []
  if (props.selected) c.push('is-selected')
  if (isRun.value) c.push('is-run', `st-${props.data.status || 'pending'}`)
  return c
})

const portClass = computed(() => {
  const c = props.data.connect
  if (!c) return ''
  return c.valid ? 'is-valid' : 'is-invalid'
})

const badge = computed(() => {
  if (isRun.value) {
    switch (props.data.status) {
      case 'completed':
        return { cls: 'is-ok', icon: 'check' }
      case 'failed':
        return { cls: 'is-err', icon: 'alert' }
      case 'waiting_human':
        return { cls: 'is-warn', icon: 'chat' }
      case 'running':
        return { cls: 'is-info', icon: 'dot' }
      default:
        return null
    }
  }
  return null
})

const issueCount = computed(() => (isRun.value ? 0 : props.data.issues?.length || 0))

const statusText = computed(() => (props.data.status ? t(`canvas.node.status.${props.data.status}`) : ''))

const renameInput = ref<HTMLInputElement | null>(null)
const renameValue = ref('')
watch(
  () => props.data.renaming,
  async (on) => {
    if (!on) return
    renameValue.value = props.data.title
    await nextTick()
    renameInput.value?.focus()
    renameInput.value?.select()
  },
  { immediate: true },
)
let renameDone = false
function commitRename(cancel = false) {
  if (renameDone) return
  renameDone = true
  ctx?.onRename(props.id, cancel ? null : renameValue.value)
  queueMicrotask(() => {
    renameDone = false
  })
}

const menuOpen = ref(false)
const menuRoot = ref<HTMLElement | null>(null)
function onDocDown(ev: MouseEvent) {
  if (menuRoot.value && !menuRoot.value.contains(ev.target as Node)) menuOpen.value = false
}
watch(menuOpen, (open) => {
  if (open) document.addEventListener('mousedown', onDocDown, true)
  else document.removeEventListener('mousedown', onDocDown, true)
})
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocDown, true))
function pick(action: NodeMenuAction) {
  menuOpen.value = false
  ctx?.onNodeMenu(props.id, action)
}
</script>

<template>
  <div
    class="cnode"
    :class="stateClass"
    :style="{ width: `${width}px` }"
    :data-testid="`canvas-node-${id}`"
    :data-node-type="data.nodeType"
    :data-status="data.status || undefined"
  >
    <Handle
      v-if="data.hasTarget"
      type="target"
      :position="Position.Left"
      class="cport cport--in"
      :class="portClass"
      :aria-label="t('canvas.aria.inPort')"
      data-testid="canvas-port-in"
    >
      <span v-if="data.connect && !data.connect.valid" class="cport-reason" role="tooltip">{{ data.connect.reason }}</span>
    </Handle>

    <div class="cnode-head">
      <slot name="icon" />
      <div class="min-w-0 flex-1">
        <input
          v-if="data.renaming"
          ref="renameInput"
          v-model="renameValue"
          class="nodrag w-full rounded-md border border-accent bg-base px-1.5 py-0.5 text-[13px] font-semibold text-txt outline-none"
          data-testid="canvas-node-rename"
          @keydown.enter.prevent="commitRename()"
          @keydown.esc.stop.prevent="commitRename(true)"
          @blur="commitRename()"
        />
        <div v-else class="cnode-title truncate" :title="data.title">{{ data.title }}</div>
        <div v-if="$slots.sub" class="cnode-sub truncate"><slot name="sub" /></div>
      </div>
      <span v-if="isRun && (data.iteration || 0) > 1" class="cnode-iter" data-testid="canvas-node-iteration">
        {{ t('canvas.node.iteration', { n: data.iteration }) }}
      </span>
      <div v-if="!isRun && ctx" ref="menuRoot" class="relative">
        <button
          type="button"
          class="cnode-menu-btn nodrag"
          :aria-label="t('canvas.aria.nodeMenu')"
          :aria-expanded="menuOpen"
          data-testid="canvas-node-menu"
          @click.stop="menuOpen = !menuOpen"
          @dblclick.stop
        >
          <Icon name="more" :size="14" />
        </button>
      </div>
    </div>

    <div v-if="menuOpen" class="cnode-menu nodrag" role="menu" @click.stop @dblclick.stop>
      <button type="button" role="menuitem" @click="pick('edit')"><Icon name="edit" :size="13" />{{ t('canvas.node.menu.edit') }}</button>
      <button type="button" role="menuitem" @click="pick('rename')"><Icon name="doc" :size="13" />{{ t('canvas.node.menu.rename') }}</button>
      <button type="button" role="menuitem" @click="pick('duplicate')"><Icon name="copy" :size="13" />{{ t('canvas.node.menu.duplicate') }}</button>
      <button type="button" role="menuitem" class="is-danger" data-testid="canvas-node-menu-delete" @click="pick('delete')"><Icon name="trash" :size="13" />{{ t('canvas.node.menu.delete') }}</button>
    </div>

    <slot />

    <div v-if="labeledOutlets" class="cnode-outlets">
      <div
        v-for="o in data.outlets"
        :key="o.id"
        class="cnode-outlet"
        :class="[`tone-${o.tone}`, { 'is-stale': o.stale }]"
        :data-testid="`canvas-outlet-${o.id || 'default'}`"
      >
        <span class="cnode-outlet-label" :title="o.label">{{ o.label }}</span>
        <Handle
          :id="o.id || undefined"
          type="source"
          :position="Position.Right"
          class="cport cport--out"
          :aria-label="t('canvas.aria.outPort', { label: o.label })"
        />
      </div>
    </div>
    <Handle
      v-else-if="plainOutlet"
      type="source"
      :position="Position.Right"
      class="cport cport--out"
      :aria-label="t('canvas.aria.outPortPlain')"
      data-testid="canvas-outlet-default"
    />

    <div v-if="isRun && data.status === 'failed' && data.failReason" class="cnode-fail" :title="data.failReason" data-testid="canvas-node-fail">
      {{ data.failReason }}
    </div>
    <div v-if="isRun && data.status === 'waiting_human' && ctx" class="cnode-reply">
      <button
        type="button"
        class="nodrag inline-flex h-6 items-center gap-1 rounded-md bg-warn px-2 text-[11px] font-medium text-base hover:opacity-90"
        data-testid="canvas-node-reply"
        @click.stop="ctx.onReply(id)"
      >
        <Icon name="chat" :size="12" />{{ t('canvas.node.reply') }}
      </button>
    </div>

    <div v-if="badge" class="cnode-badge" :class="badge.cls" :title="statusText" :aria-label="statusText">
      <Icon :name="badge.icon" :size="11" />
    </div>
    <template v-else-if="issueCount">
      <div class="cnode-badge is-err" :aria-label="t('canvas.node.issues', { n: issueCount })" data-testid="canvas-node-issues">{{ issueCount }}</div>
      <ul class="cnode-issues" role="tooltip">
        <li v-for="(msg, i) in data.issues" :key="i">{{ msg }}</li>
      </ul>
    </template>
  </div>
</template>
