<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AgentIcon } from '@/lib/api/api'
import { agentHueVar, agentInitial } from '@/components/canvas/composables/agentAvatar'
import AgentIdentityGlyph from './AgentIdentityGlyph.vue'
import {
  IDENTITY_COLORS,
  identityColor,
  identityIcon,
  identityIconLabel,
  identityTint,
  searchIdentityIcons,
  type IdentityColorId,
} from '@/lib/agent/identityIcons'

const props = withDefaults(
  defineProps<{
    modelValue?: AgentIcon | null
    name?: string
    size?: 'sm' | 'lg'
    purpose?: 'choose' | 'change'
  }>(),
  { modelValue: null, name: '', size: 'sm', purpose: 'choose' },
)

const emit = defineEmits<{ (e: 'update:modelValue', value: AgentIcon | null): void }>()

const { t, locale } = useI18n()
const open = ref(false)
const query = ref('')
const color = ref<IdentityColorId>('black')
const root = ref<HTMLElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const searchEl = ref<HTMLInputElement | null>(null)
const panelStyle = ref<Record<string, string>>({})

const shown = computed(() => {
  const icon = props.modelValue
  if (!icon || !identityIcon(icon.iconId) || !identityColor(icon.color)) return null
  return icon
})

const filtered = computed(() => searchIdentityIcons(query.value))
const activeColor = computed(() => identityColor(color.value) ?? IDENTITY_COLORS[0]!)
const glyphSize = computed(() => (props.size === 'lg' ? 20 : 14))

const slotStyle = computed(() => {
  if (shown.value) return identityTint(shown.value.color) ?? {}
  if (props.size === 'lg' && !props.name.trim()) return { color: 'rgb(var(--c-accent-2))', background: 'transparent' }
  const hue = agentHueVar(props.name)
  return { background: `rgb(var(${hue}) / 0.16)`, color: `rgb(var(${hue}))` }
})

function savedColor(): IdentityColorId {
  const saved = props.modelValue?.color
  if (saved && identityColor(saved)) return saved as IdentityColorId
  return 'black'
}

function place() {
  const el = root.value?.querySelector('button')
  if (!el) return
  const rect = el.getBoundingClientRect()
  const width = Math.min(312, window.innerWidth - 32)
  const left = Math.max(16, Math.min(rect.left, window.innerWidth - width - 16))
  const below = window.innerHeight - rect.bottom
  const style: Record<string, string> = {
    position: 'fixed',
    left: `${Math.round(left)}px`,
    width: `${Math.round(width)}px`,
    zIndex: '70',
  }
  if (below < 280 && rect.top > below) style.bottom = `${Math.round(window.innerHeight - rect.top + 8)}px`
  else style.top = `${Math.round(rect.bottom + 8)}px`
  panelStyle.value = style
}

function onPointerDown(event: Event) {
  const target = event.target as Node | null
  if (!target) return
  if (root.value?.contains(target) || panel.value?.contains(target)) return
  closePicker()
}

function onKey(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !open.value) return
  event.preventDefault()
  event.stopPropagation()
  closePicker()
}

function bind() {
  document.addEventListener('pointerdown', onPointerDown, true)
  document.addEventListener('keydown', onKey, true)
  window.addEventListener('resize', place)
  window.addEventListener('scroll', place, true)
}

function unbind() {
  document.removeEventListener('pointerdown', onPointerDown, true)
  document.removeEventListener('keydown', onKey, true)
  window.removeEventListener('resize', place)
  window.removeEventListener('scroll', place, true)
}

function closePicker() {
  if (!open.value) return
  open.value = false
  unbind()
}

async function openPicker() {
  color.value = savedColor()
  query.value = ''
  open.value = true
  await nextTick()
  place()
  bind()
  searchEl.value?.focus()
}

function toggle() {
  if (open.value) closePicker()
  else void openPicker()
}

function pick(id: string) {
  emit('update:modelValue', { iconId: id, color: color.value })
  closePicker()
}

function clear() {
  emit('update:modelValue', null)
  closePicker()
}

onBeforeUnmount(unbind)
</script>

<template>
  <span ref="root" class="identity-anchor">
    <button
      type="button"
      data-test="agent-identity-mark"
      class="identity-mark"
      :class="[size, { has: !!shown }]"
      :aria-label="t(purpose === 'change' ? 'pages.agentStudio.identityIcon.change' : 'pages.agentStudio.identityIcon.choose')"
      :aria-expanded="open"
      @click="toggle"
    >
      <span class="identity-slot" :style="slotStyle">
        <AgentIdentityGlyph v-if="shown" :icon-id="shown.iconId" :size="glyphSize" />
        <svg
          v-else-if="size === 'lg' && !name.trim()"
          class="identity-plus"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <path d="M12 6v12M6 12h12" />
        </svg>
        <span v-else class="identity-letter">{{ agentInitial(name) }}</span>
      </span>
    </button>
    <Teleport to="body">
      <div
        v-if="open"
        ref="panel"
        data-test="agent-identity-picker"
        class="identity-picker"
        :style="panelStyle"
        role="dialog"
        :aria-label="t('pages.agentStudio.identityIcon.choose')"
      >
        <div class="identity-search">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <circle cx="11" cy="11" r="6" />
            <path d="m20 20-3.5-3.5" />
          </svg>
          <input
            ref="searchEl"
            v-model="query"
            data-test="agent-identity-search"
            type="text"
            :placeholder="t('pages.agentStudio.identityIcon.search')"
          />
        </div>
        <div class="identity-colors">
          <button
            v-for="item in IDENTITY_COLORS"
            :key="item.id"
            type="button"
            class="identity-dot"
            :class="{ on: color === item.id }"
            :style="{ background: item.hex }"
            :data-test="`agent-identity-color-${item.id}`"
            :aria-label="t(`pages.agentStudio.identityIcon.colors.${item.id}`)"
            :aria-pressed="color === item.id"
            @click="color = item.id"
          />
        </div>
        <div v-if="filtered.length" class="identity-grid" :style="{ color: activeColor.hex }">
          <button
            v-for="icon in filtered"
            :key="icon.id"
            type="button"
            class="identity-cell"
            :class="{ on: shown?.iconId === icon.id }"
            :data-test="`agent-identity-icon-${icon.id}`"
            :aria-label="identityIconLabel(icon, locale)"
            @click="pick(icon.id)"
          >
            <AgentIdentityGlyph :icon-id="icon.id" :size="18" />
          </button>
        </div>
        <p v-else data-test="agent-identity-empty" class="identity-empty">
          {{ t('pages.agentStudio.identityIcon.empty') }}
        </p>
        <div class="identity-foot">
          <button type="button" data-test="agent-identity-clear" class="identity-clear" @click="clear">
            {{ t('pages.agentStudio.identityIcon.clear') }}
          </button>
          <span>{{ t(`pages.agentStudio.identityIcon.colors.${color}`) }}</span>
        </div>
      </div>
    </Teleport>
  </span>
</template>

<style scoped>
.identity-anchor {
  position: relative;
  display: inline-flex;
  flex: none;
}
.identity-mark {
  display: grid;
  place-items: center;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
}
.identity-mark.sm {
  width: 28px;
  height: 28px;
  border-radius: 8px;
}
.identity-mark.lg {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  border: 1px dashed rgb(var(--c-line-strong));
  background: rgb(var(--c-surface));
}
.identity-mark.lg.has {
  border-color: transparent;
  background: transparent;
}
.identity-mark:hover {
  background: rgb(var(--c-elevated));
}
.identity-mark.lg:hover {
  border-color: rgb(var(--c-accent));
  background: rgb(var(--c-accent) / 0.08);
}
.identity-slot {
  display: grid;
  place-items: center;
  border-radius: 7px;
}
.identity-mark.sm .identity-slot {
  width: 22px;
  height: 22px;
}
.identity-mark.lg .identity-slot {
  width: 40px;
  height: 40px;
  border-radius: 12px;
}
.identity-letter {
  font-weight: 700;
  letter-spacing: -0.02em;
  line-height: 1;
}
.identity-mark.sm .identity-letter {
  font-size: 11px;
}
.identity-mark.lg .identity-letter {
  font-size: 16px;
}
.identity-plus {
  width: 20px;
  height: 20px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.6;
  stroke-linecap: round;
}
.identity-picker {
  padding: 12px;
  border-radius: 16px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt));
  box-shadow: var(--shadow-card, 0 18px 50px rgba(24, 24, 27, 0.12));
}
.identity-search {
  position: relative;
}
.identity-search svg {
  position: absolute;
  left: 10px;
  top: 9px;
  width: 14px;
  height: 14px;
  fill: none;
  stroke: rgb(var(--c-txt3));
  stroke-width: 1.6;
  stroke-linecap: round;
}
.identity-search input {
  width: 100%;
  height: 34px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 10px;
  padding: 0 10px 0 32px;
  background: rgb(var(--c-surface));
  color: inherit;
  font: inherit;
  font-size: 12px;
  outline: none;
}
.identity-search input:focus {
  border-color: rgb(var(--c-accent));
  box-shadow: 0 0 0 3px rgb(var(--c-accent) / 0.18);
}
.identity-colors {
  display: flex;
  justify-content: space-between;
  margin: 12px 4px 10px;
}
.identity-dot {
  width: 18px;
  height: 18px;
  padding: 0;
  border: 0;
  border-radius: 50%;
}
.identity-dot.on {
  box-shadow: 0 0 0 2px rgb(var(--c-surface)), 0 0 0 3.5px rgb(var(--c-txt));
}
.identity-grid {
  display: grid;
  grid-template-columns: repeat(8, 1fr);
  gap: 2px;
}
.identity-cell {
  height: 34px;
  border: 0;
  border-radius: 9px;
  background: transparent;
  color: inherit;
  display: grid;
  place-items: center;
}
.identity-cell:hover,
.identity-cell.on {
  background: color-mix(in srgb, currentColor 12%, transparent);
}
.identity-empty {
  margin: 0;
  padding: 16px 4px;
  text-align: center;
  color: rgb(var(--c-txt3));
  font-size: 12px;
}
.identity-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 8px;
  padding-top: 6px;
  color: rgb(var(--c-txt3));
  font-size: 11px;
}
.identity-clear {
  border: 0;
  background: transparent;
  color: rgb(var(--c-txt2));
  font: inherit;
  font-size: 12px;
  padding: 4px 2px;
  border-radius: 6px;
}
.identity-clear:hover {
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt));
}
</style>
