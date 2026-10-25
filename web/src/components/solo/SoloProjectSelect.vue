<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import SoloPickerShell from './SoloPickerShell.vue'
import { repoShort } from '@/lib/solo/soloPrefs'
import { repoTriggerLabel } from '@/lib/solo/sessionRepos'
import type { Project } from '@/lib/api/apiTypes'

const props = withDefaults(
  defineProps<{
    projects?: Project[]
    modelValue?: string[]
    /** Kept so parents can still pass branch overrides. Empty means each repo's default branch at launch. */
    branches?: Record<string, string>
    disabled?: boolean
    loading?: boolean
  }>(),
  { projects: () => [], modelValue: () => [], branches: () => ({}), disabled: false, loading: false },
)
const emit = defineEmits<{
  (e: 'update:modelValue', v: string[]): void
  (e: 'update:branch', id: string, branch: string): void
  (e: 'create'): void
}>()
const { t } = useI18n()

const shellRef = ref<ComponentPublicInstance | null>(null)
const stripEl = ref<HTMLElement | null>(null)
const selected = computed(() => (Array.isArray(props.modelValue) ? props.modelValue : []))
const noneLabel = computed(() => t('pages.solo.projectSelect.none'))
const nameText = computed(() => {
  if (!selected.value.length) return noneLabel.value
  const id = selected.value[0]!
  return props.projects.find((project) => project.id === id)?.name || id
})
const extra = computed(() => Math.max(0, selected.value.length - 1))
const plainLabel = computed(() =>
  repoTriggerLabel(selected.value, (id) => props.projects.find((project) => project.id === id)?.name || '', noneLabel.value),
)
const triggerTitle = computed(() => {
  if (!selected.value.length) return noneLabel.value
  return selected.value.map((id) => props.projects.find((project) => project.id === id)?.name || id).join('、')
})
const motionKey = computed(() => `${nameText.value}\0${extra.value}`)

let shown = 0
let ready = false
let pendingFrom = 0
let pendingAnimate = false

function triggerButton(): HTMLButtonElement | null {
  const root = shellRef.value?.$el
  if (!(root instanceof HTMLElement)) return null
  return root.querySelector<HTMLButtonElement>('[data-testid="solo-project-select-trigger"]')
}

function prefersReducedMotion() {
  return typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
}

function rollCell(text: string) {
  const span = document.createElement('span')
  span.textContent = text
  span.style.height = '16px'
  span.style.lineHeight = '16px'
  return span
}

/** +N rolls only the digit. Larger values enter from below; smaller ones from above. */
function rollTo(next: number, animate: boolean) {
  const strip = stripEl.value
  if (next <= 0 || !strip) {
    shown = 0
    return
  }
  const motion = animate && !prefersReducedMotion() && shown !== next
  if (!motion) {
    strip.style.transition = 'none'
    strip.replaceChildren(rollCell(String(next)))
    strip.style.transform = 'none'
    shown = next
    return
  }
  const dir = next > shown ? 1 : -1
  strip.style.transition = 'none'
  if (dir > 0) {
    strip.replaceChildren(rollCell(shown > 0 ? String(shown) : ''), rollCell(String(next)))
    strip.style.transform = 'translateY(0)'
  } else {
    strip.replaceChildren(rollCell(String(next)), rollCell(String(shown)))
    strip.style.transform = 'translateY(-50%)'
  }
  shown = next
  void strip.offsetWidth
  strip.style.transition = ''
  strip.style.transform = dir > 0 ? 'translateY(-50%)' : 'translateY(0)'
}

/** Lock the button's current width, then ease to the width of the new label. */
function glide(button: HTMLButtonElement | null, from: number) {
  if (!button) return
  if (prefersReducedMotion()) {
    button.style.transition = ''
    button.style.width = ''
    return
  }
  if (from < 1) return
  button.style.transition = 'none'
  button.style.width = 'max-content'
  const to = button.getBoundingClientRect().width
  if (to < 1 || Math.abs(from - to) < 1) {
    button.style.transition = ''
    button.style.width = to >= 1 ? `${to}px` : ''
    return
  }
  button.style.width = `${from}px`
  void button.offsetWidth
  button.style.transition = 'width 220ms var(--ease-out-expo), border-color 0.18s ease, background-color 0.18s ease'
  button.style.width = `${to}px`
}

watch(motionKey, () => {
  const button = triggerButton()
  pendingFrom = ready && button ? button.getBoundingClientRect().width : 0
  pendingAnimate = ready
}, { flush: 'pre' })

watch(motionKey, () => {
  const animate = pendingAnimate
  rollTo(extra.value, animate)
  if (animate) glide(triggerButton(), pendingFrom)
  ready = true
}, { flush: 'post' })

onMounted(() => {
  rollTo(extra.value, false)
  ready = true
})

function matches(project: Project, query: string) {
  return !query || project.name.toLowerCase().includes(query) || repoShort(project.repoUrl).toLowerCase().includes(query)
}

function isOn(id: string) {
  return selected.value.includes(id)
}

function toggle(id: string) {
  const current = selected.value
  emit('update:modelValue', current.includes(id) ? current.filter((item) => item !== id) : [...current, id])
}

function onRowKeydown(event: KeyboardEvent, id: string) {
  if (event.target instanceof HTMLInputElement) return
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    toggle(id)
  }
}
</script>

<template>
  <SoloPickerShell
    ref="shellRef"
    class="solo-repo-picker"
    :label="plainLabel"
    :placeholder="!selected.length"
    icon="git"
    multiple
    :disabled="disabled"
    :search-placeholder="projects.length > 5 ? t('pages.solo.projectSelect.search') : ''"
    :panel-width="300"
    test-id="solo-project-select"
  >
    <template #label>
      <span class="solo-picker__label solo-repo-label" :class="{ 'is-placeholder': !selected.length }" :title="triggerTitle">
        <span class="solo-repo-name">{{ nameText }}</span>
        <span v-if="extra > 0" class="solo-repo-count">
          <span class="solo-repo-plus"> +</span>
          <span class="solo-repo-roll">
            <span ref="stripEl" class="solo-repo-roll-strip" />
          </span>
        </span>
      </span>
    </template>
    <template #default="{ query }">
      <div
        v-for="project in projects.filter((item) => matches(item, query))"
        :key="project.id"
        role="option"
        tabindex="0"
        data-picker-opt
        class="solo-project-opt"
        :aria-selected="isOn(project.id)"
        :data-testid="`solo-project-option-${project.id}`"
        :title="repoShort(project.repoUrl)"
        @click="toggle(project.id)"
        @keydown="onRowKeydown($event, project.id)"
      >
        <Icon name="git" :size="15" class="solo-project-ico" />
        <span class="solo-project-body">
          <span class="solo-project-name">{{ project.name }}</span>
          <span class="solo-project-sub">{{ repoShort(project.repoUrl) }}</span>
        </span>
        <Icon name="check" :size="14" class="solo-project-tick" :class="{ 'is-on': isOn(project.id) }" />
      </div>
      <div v-if="loading && !projects.length" class="solo-picker-empty">
        <Icon name="spinner" :size="14" class="inline" />
      </div>
      <div v-else-if="projects.length && !projects.some((item) => matches(item, query))" class="solo-picker-empty">
        {{ t('pages.solo.projectSelect.empty') }}
      </div>
    </template>
    <template #footer="{ close }">
      <div class="solo-project-foot">
        <button type="button" class="solo-project-act" data-testid="solo-project-create" @click="close(false); emit('create')">
          {{ t('pages.solo.projectSelect.create') }}
        </button>
        <RouterLink to="/settings/projects" class="solo-project-act" @click="close(false)">
          {{ t('pages.solo.projectSelect.manage') }}
        </RouterLink>
      </div>
    </template>
  </SoloPickerShell>
</template>

<style scoped>
.solo-repo-picker {
  width: max-content;
  max-width: 15rem;
}
.solo-repo-picker :deep(.solo-picker__trigger) {
  width: max-content;
  max-width: 15rem;
}
.solo-repo-label {
  display: inline-flex;
  align-items: center;
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
}
.solo-repo-label.is-placeholder {
  color: rgb(var(--c-txt3));
}
.solo-repo-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.solo-repo-count {
  display: inline-flex;
  align-items: center;
  flex: none;
  height: 16px;
  font-variant-numeric: tabular-nums;
}
.solo-repo-roll {
  height: 16px;
  overflow: hidden;
  line-height: 16px;
}
.solo-repo-roll-strip {
  display: flex;
  flex-direction: column;
  transition: transform 220ms var(--ease-out-expo);
}
.solo-repo-roll-strip :deep(span) {
  height: 16px;
  line-height: 16px;
}
div.solo-project-opt.solo-project-opt {
  height: 52px;
  min-height: 52px;
  box-sizing: border-box;
  align-items: center;
  gap: 8px;
  padding: 0 8px;
  overflow: hidden;
}
div.solo-project-opt.solo-project-opt:hover,
div.solo-project-opt.solo-project-opt:focus-visible {
  background: rgb(var(--c-accent) / 0.05);
}
div.solo-project-opt.solo-project-opt[aria-selected='true'],
div.solo-project-opt.solo-project-opt[aria-selected='true']:hover,
div.solo-project-opt.solo-project-opt[aria-selected='true']:focus-visible {
  border-color: transparent;
  background: rgb(var(--c-accent) / 0.06);
}
.solo-project-ico {
  flex: none;
  color: rgb(var(--c-txt3));
}
div.solo-project-opt[aria-selected='true'] .solo-project-ico {
  color: rgb(var(--c-accent));
}
.solo-project-body {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  justify-content: center;
  gap: 2px;
  height: 36px;
}
.solo-project-name,
.solo-project-sub {
  display: block;
  height: 16px;
  min-width: 0;
  overflow: hidden;
  line-height: 16px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.solo-project-name {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: -0.011em;
}
.solo-project-sub {
  color: rgb(var(--c-txt3));
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
}
.solo-project-tick {
  flex: none;
  color: rgb(var(--c-accent));
  opacity: 0;
}
.solo-project-tick.is-on {
  opacity: 1;
}
.solo-project-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.solo-project-act {
  display: inline-flex;
  height: 32px;
  align-items: center;
  border: 0;
  border-radius: 8px;
  background: transparent;
  padding: 0 8px;
  color: rgb(var(--c-txt2));
  font-size: 13px;
  text-decoration: none;
  cursor: pointer;
}
.solo-project-act:hover {
  background: rgb(var(--c-accent) / 0.05);
}
@media (prefers-reduced-motion: reduce) {
  .solo-repo-roll-strip {
    transition: none;
  }
}
</style>
