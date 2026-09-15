<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { ArtifactVersionChoice } from '@/lib/run/reactArtifactPreview'

const props = withDefaults(
  defineProps<{
    choices: ArtifactVersionChoice[]
    selectedIndex?: number | null
    currentLabel: string
    menuAriaLabel: string
    chipTestId?: string
    buttonTestId?: string
    menuTestId?: string
    optionTestIdPrefix?: string
    labelFor: (choice: ArtifactVersionChoice) => string
  }>(),
  {
    selectedIndex: null,
    chipTestId: 'artifact-version-chip',
    buttonTestId: 'artifact-version-chip-btn',
    menuTestId: 'artifact-version-menu',
    optionTestIdPrefix: 'artifact-version-option-v',
  },
)

const emit = defineEmits<{
  select: [choice: ArtifactVersionChoice]
}>()

const MARGIN = 8
const GAP = 4
const MAX_PANEL_HEIGHT = 320

const open = ref(false)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const panelStyle = ref<Record<string, string>>({})
const placement = ref<'above' | 'below'>('below')

function placePanel() {
  const trig = trigger.value
  if (!trig) return
  const r = trig.getBoundingClientRect()
  const width = Math.max(120, Math.min(220, r.width + 48))
  let left = r.right - width
  left = Math.max(MARGIN, Math.min(left, window.innerWidth - width - MARGIN))

  const spaceAbove = r.top - GAP - MARGIN
  const spaceBelow = window.innerHeight - r.bottom - GAP - MARGIN
  // Prefer below (like AppSelect). Flip only when the menu cannot fit underneath.
  const measured = panel.value?.offsetHeight || 0
  const needed = Math.min(measured || MAX_PANEL_HEIGHT, MAX_PANEL_HEIGHT)
  const flipUp = spaceBelow < needed && spaceAbove > spaceBelow
  placement.value = flipUp ? 'above' : 'below'

  const maxH = Math.min(
    MAX_PANEL_HEIGHT,
    flipUp ? Math.max(80, spaceAbove) : Math.max(80, spaceBelow),
    window.innerHeight - MARGIN * 2,
  )
  // When flipping up, hug the chip with the actual (clamped) height — not maxH alone,
  // so a short 5-item list does not float to the viewport top.
  const height = Math.min(measured || needed, maxH)
  let top = flipUp ? r.top - GAP - height : r.bottom + GAP
  top = Math.max(MARGIN, Math.min(top, window.innerHeight - MARGIN - Math.min(height, maxH)))

  panelStyle.value = {
    position: 'fixed',
    top: `${Math.round(top)}px`,
    left: `${Math.round(left)}px`,
    width: `${Math.round(width)}px`,
    maxHeight: `${Math.round(maxH)}px`,
    overflowY: 'auto',
  }
}

function onScrollOrResize() {
  if (open.value) placePanel()
}

async function toggle() {
  open.value = !open.value
  if (open.value) {
    // First nextTick mounts the teleported panel; second pass remeasures after styles.
    await nextTick()
    placePanel()
    await nextTick()
    placePanel()
  }
}

function close() {
  open.value = false
}

function select(choice: ArtifactVersionChoice) {
  if (!choice.available) return
  emit('select', choice)
  close()
}

function onDocClick(e: MouseEvent) {
  if (!open.value) return
  const el = e.target as HTMLElement | null
  if (el?.closest?.(`[data-testid="${props.chipTestId}"]`)) return
  if (el?.closest?.(`[data-testid="${props.menuTestId}"]`)) return
  close()
}

watch(
  () => props.choices.map((c) => `${c.index}:${c.available ? 1 : 0}`).join(','),
  () => {
    if (open.value) void nextTick(placePanel)
  },
)

onMounted(() => {
  document.addEventListener('click', onDocClick)
  window.addEventListener('resize', onScrollOrResize)
  window.addEventListener('scroll', onScrollOrResize, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  window.removeEventListener('resize', onScrollOrResize)
  window.removeEventListener('scroll', onScrollOrResize, true)
})
</script>

<template>
  <div class="relative shrink-0" :data-testid="chipTestId" @click.stop>
    <button
      ref="trigger"
      type="button"
      class="rounded-md inline-flex items-center gap-0.5 border border-line bg-elevated px-1.5 py-px text-[10px] text-txt2 hover:border-line-strong hover:text-txt"
      :class="{ 'border-accent/60 text-txt': open }"
      :aria-expanded="open ? 'true' : 'false'"
      aria-haspopup="listbox"
      :aria-label="menuAriaLabel"
      :data-testid="buttonTestId"
      @click.stop="toggle"
    >
      <span>{{ currentLabel }}</span>
    </button>
    <Teleport to="body">
      <div
        v-if="open"
        ref="panel"
        role="listbox"
        class="artifact-version-select__menu rounded-lg border border-line bg-surface py-0.5"
        :data-testid="menuTestId"
        :data-placement="placement"
        :style="panelStyle"
        @click.stop
      >
        <button
          v-for="choice in choices"
          :key="choice.index"
          type="button"
          role="option"
          class="flex w-full items-center px-2.5 py-1.5 text-left text-[11px] transition"
          :class="
            !choice.available
              ? 'cursor-not-allowed text-txt3 opacity-45'
              : selectedIndex === choice.index
                ? 'bg-accent-dim text-txt'
                : 'text-txt2 hover:bg-elevated'
          "
          :aria-selected="selectedIndex === choice.index ? 'true' : 'false'"
          :disabled="!choice.available"
          :data-testid="optionTestIdPrefix + choice.index"
          @click.stop="select(choice)"
        >
          {{ labelFor(choice) }}
        </button>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.artifact-version-select__menu {
  z-index: 60;
  overflow-x: hidden;
  overflow-y: auto;
  min-width: 7.5rem;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.35);
}
</style>
