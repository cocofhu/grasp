<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '../../ui/Icon.vue'
import CredentialProviderLogo from '@/components/project/CredentialProviderLogo.vue'
import type { ProjectCredentialItem } from '@/lib/api/api'
import { credentialModel, credentialVendor } from './modelCredential'

const props = defineProps<{
  /** Selected credential id; empty follows the Agent. */
  modelValue: string
  rows: ProjectCredentialItem[]
  inherited: ProjectCredentialItem | null
  loading: boolean
  projectId?: string
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const { t } = useI18n()
const open = ref(false)
const active = ref(0)
const root = ref<HTMLElement | null>(null)
const list = ref<HTMLElement | null>(null)

const selected = computed(() => props.rows.find((r) => r.id === props.modelValue) ?? null)
const stale = computed(() => !!props.modelValue && !props.loading && !selected.value)
/** Index 0 is "follow Agent"; credential rows start at 1. */
const options = computed(() => ['', ...props.rows.map((r) => r.id)])
const manageLink = computed(() => ({ path: '/settings/credentials' }))

function followLine(): string {
  const c = props.inherited
  return c ? t('canvas.inspector.model.followCurrent', { name: c.name, model: credentialVendor(c) }) : t('canvas.inspector.model.followEmpty')
}

async function toggle() {
  open.value = !open.value
  if (!open.value) return
  active.value = Math.max(0, options.value.indexOf(props.modelValue))
  await nextTick()
  list.value?.focus()
}

function pick(id: string) {
  if (id !== props.modelValue) emit('update:modelValue', id)
  open.value = false
}

function onKey(e: KeyboardEvent) {
  const n = options.value.length
  if (e.key === 'ArrowDown') active.value = (active.value + 1) % n
  else if (e.key === 'ArrowUp') active.value = (active.value - 1 + n) % n
  else if (e.key === 'Enter') pick(options.value[active.value]!)
  else if (e.key === 'Escape') open.value = false
  else return
  e.preventDefault()
  e.stopPropagation()
}

function onDocDown(ev: MouseEvent) {
  if (open.value && root.value && !root.value.contains(ev.target as Node)) open.value = false
}
onMounted(() => document.addEventListener('mousedown', onDocDown, true))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocDown, true))
</script>

<template>
  <div ref="root" class="relative">
    <button
      type="button"
      class="input flex h-auto min-h-[48px] items-center gap-2.5 py-1.5 text-left"
      :class="stale || (!modelValue && !inherited && !loading) ? '!border-warn/60' : ''"
      :aria-expanded="open"
      aria-haspopup="listbox"
      :aria-label="t('canvas.inspector.model.credential')"
      data-testid="model-picker-trigger"
      @click="toggle"
    >
      <template v-if="selected">
        <CredentialProviderLogo :provider="credentialVendor(selected) || 'opencode'" match="provider" :configured="true" selected />
        <span class="min-w-0 flex-1">
          <span class="flex items-center gap-1.5">
            <span class="truncate text-[13px] font-medium text-txt">{{ selected.name }}</span>
            <span class="inline-flex h-[18px] shrink-0 items-center rounded border border-accent/35 bg-accent-dim px-1.5 text-[10px] leading-none text-accent-2" data-testid="model-picker-node-chip">{{ t('canvas.inspector.model.nodeChip') }}</span>
          </span>
          <span class="block truncate font-mono text-[11px] text-txt3">{{ selected.masked || credentialVendor(selected) }}</span>
        </span>
      </template>
      <template v-else-if="stale">
        <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-warn/10 text-warn"><Icon name="alert" :size="16" /></span>
        <span class="min-w-0 flex-1">
          <span class="block truncate text-[13px] font-medium text-warn">{{ t('canvas.inspector.model.staleTitle') }}</span>
          <span class="block truncate text-[11px] text-txt3">{{ t('canvas.inspector.model.staleBody') }}</span>
        </span>
      </template>
      <template v-else>
        <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-dashed border-line text-txt3"><Icon name="robot" :size="16" /></span>
        <span class="min-w-0 flex-1">
          <span class="block truncate text-[13px] font-medium text-txt">{{ t('canvas.inspector.model.follow') }}</span>
          <span v-if="loading" class="block truncate text-[11px] text-txt3">{{ t('common.loading.inProgress') }}</span>
          <span v-else class="block truncate text-[11px]" :class="inherited ? 'text-txt3' : 'text-warn'" data-testid="model-picker-follow-line">{{ followLine() }}</span>
        </span>
      </template>
      <Icon name="chevron-down" :size="14" class="shrink-0 text-txt3" />
    </button>

    <div
      v-if="open"
      ref="list"
      class="cchrome scroll-area absolute left-0 right-0 z-30 mt-1 max-h-80 overflow-y-auto p-1 outline-none"
      role="listbox"
      tabindex="-1"
      :aria-activedescendant="`cred-opt-${active}`"
      data-testid="model-picker-list"
      @keydown="onKey"
    >
      <button
        id="cred-opt-0"
        type="button"
        role="option"
        :aria-selected="!modelValue"
        class="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left"
        :class="active === 0 ? 'bg-elevated' : ''"
        data-testid="model-option-follow"
        @mouseenter="active = 0"
        @click="pick('')"
      >
        <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-dashed border-line text-txt3"><Icon name="robot" :size="14" /></span>
        <span class="min-w-0 flex-1">
          <span class="block truncate text-[13px] text-txt">{{ t('canvas.inspector.model.follow') }}</span>
          <span class="block truncate text-[11px] text-txt3">{{ followLine() }}</span>
        </span>
        <Icon v-if="!modelValue" name="check" :size="14" class="shrink-0 text-accent-2" />
      </button>

      <p class="mx-2 mb-1 mt-2 border-t border-line pt-2 text-[10px] font-semibold uppercase tracking-[0.12em] text-txt3">
        {{ t('canvas.inspector.model.projectKeys') }}
      </p>
      <p v-if="!rows.length" class="px-3 py-3 text-center text-[12px] text-txt3" data-testid="model-picker-empty">{{ t('canvas.inspector.model.empty') }}</p>
      <button
        v-for="(item, i) in rows"
        :id="`cred-opt-${i + 1}`"
        :key="item.id"
        type="button"
        role="option"
        :aria-selected="item.id === modelValue"
        class="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left"
        :class="active === i + 1 ? 'bg-elevated' : ''"
        :data-testid="`model-option-${item.id}`"
        @mouseenter="active = i + 1"
        @click="pick(item.id)"
      >
        <CredentialProviderLogo :provider="credentialVendor(item) || 'opencode'" match="provider" :configured="true" :selected="item.id === modelValue" />
        <span class="min-w-0 flex-1">
          <span class="flex items-center gap-1.5">
            <span class="truncate text-[13px] text-txt">{{ item.name }}</span>
            <span v-if="item.id === inherited?.id" class="inline-flex h-[18px] shrink-0 items-center rounded border border-line bg-surface px-1.5 text-[10px] leading-none text-txt3">{{ t('canvas.inspector.model.agentDefault') }}</span>
          </span>
          <span class="block truncate text-[11px] text-txt3">
            {{ t('canvas.inspector.model.credentialLine', { vendor: credentialVendor(item), model: credentialModel(item) }) }}
          </span>
        </span>
        <Icon v-if="item.id === modelValue" name="check" :size="14" class="shrink-0 text-accent-2" />
      </button>

      <RouterLink
        v-if="manageLink"
        :to="manageLink"
        class="mt-1 flex items-center gap-1.5 rounded-md border-t border-line px-2 pb-1 pt-2 text-[12px] text-accent-2 hover:underline"
        data-testid="model-picker-manage"
      >
        <Icon name="plus" :size="12" />{{ t('canvas.inspector.model.manage') }}
      </RouterLink>
    </div>
  </div>
</template>
