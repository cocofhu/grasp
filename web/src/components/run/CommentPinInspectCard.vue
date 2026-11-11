<script setup lang="ts">
import { computed, ref, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import type { InspectElementStyle } from '@/lib/shared/htmlPreviewSandbox'

const props = defineProps<{
  open: boolean
  selector: string
  imageDataUrl?: string
  screenshotMissing?: boolean
  /** Initial comment when editing an existing pin. */
  initialComment?: string
  /**
   * Anchor rect relative to the positioning container (preview wrap).
   * Used by placeCardNear to flip/clamp inside the container.
   */
  anchor?: { left: number; top: number; width: number; height: number } | null
  containerEl?: HTMLElement | null
  /** Computed style from inspect pick (Open Design Size/Color/Font/Line rows). */
  styleInfo?: InspectElementStyle | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'save', comment: string): void
  (e: 'send-chat', comment: string): void
}>()

const { t } = useI18n()
const cardRef = ref<HTMLElement | null>(null)
const comment = ref('')
const top = ref(8)
const left = ref(8)

const canSubmit = computed(() => comment.value.trim().length > 0)

/** Demo dark elevated surface; wins over html.light token inheritance. */
const DEMO_DARK_SURFACE_VARS = {
  '--c-base': '10 10 11',
  '--c-elevated': '28 28 33',
  '--c-line': '38 38 43',
  '--c-line-strong': '54 54 62',
  '--c-txt': '237 237 240',
  '--c-txt2': '161 161 170',
  '--c-txt3': '110 110 120',
  '--shadow-card': '0 1px 2px 0 rgba(0, 0, 0, 0.4), 0 1px 3px 0 rgba(0, 0, 0, 0.25)',
} as const

const cardStyle = computed(() => ({
  top: `${top.value}px`,
  left: `${left.value}px`,
  colorScheme: 'dark' as const,
  ...DEMO_DARK_SURFACE_VARS,
}))

type StyleRow = { label: string; value: string; swatch?: string }

function cssColorToHex(value: string | undefined): string | null {
  if (!value) return null
  const raw = value.trim()
  if (!raw || raw === 'transparent' || raw === 'rgba(0, 0, 0, 0)') return null
  if (/^#[0-9a-f]{3}([0-9a-f]{3})?$/i.test(raw)) {
    if (raw.length === 4) {
      return '#' + raw.slice(1).split('').map((char) => char + char).join('').toUpperCase()
    }
    return raw.toUpperCase()
  }
  const match = raw.match(/rgba?\(\s*([0-9.]+)[ ,]+([0-9.]+)[ ,]+([0-9.]+)/i)
  if (!match) return raw
  const toHex = (part: string | undefined) => {
    const n = Math.max(0, Math.min(255, Math.round(Number(part ?? 0))))
    return n.toString(16).padStart(2, '0').toUpperCase()
  }
  return `#${toHex(match[1])}${toHex(match[2])}${toHex(match[3])}`
}

function compactFontFamily(value: string | undefined): string | null {
  if (!value) return null
  const first = value.split(',')[0]?.trim().replace(/^["']|["']$/g, '')
  return first || null
}

const styleRows = computed((): StyleRow[] => {
  const rows: StyleRow[] = []
  const a = props.anchor
  if (a && a.width > 0 && a.height > 0) {
    rows.push({ label: 'Size', value: `${Math.round(a.width)}x${Math.round(a.height)}` })
  }
  const st = props.styleInfo
  if (!st) return rows
  const color = cssColorToHex(st.color)
  if (color) rows.push({ label: 'Color', value: color, swatch: color })
  const fontParts = [
    st.fontSize,
    st.fontWeight && st.fontWeight !== '400' ? st.fontWeight : null,
    compactFontFamily(st.fontFamily),
  ].filter((part): part is string => Boolean(part))
  if (fontParts.length > 0) {
    rows.push({ label: 'Font', value: fontParts.join(' ') })
  }
  if (st.lineHeight) rows.push({ label: 'Line', value: st.lineHeight })
  return rows
})

watch(
  () => props.open,
  (v) => {
    if (v) {
      comment.value = props.initialComment || ''
      nextTick(() => placeCardNear())
    }
  },
)

watch(
  () => [props.anchor, props.initialComment] as const,
  () => {
    if (!props.open) return
    if (props.initialComment != null) comment.value = props.initialComment
    nextTick(() => placeCardNear())
  },
)

function placeCardNear() {
  const card = cardRef.value
  const wrap = props.containerEl
  if (!card || !wrap) return
  const wrapRect = wrap.getBoundingClientRect()
  const cardH = card.offsetHeight || 280
  const cardW = card.offsetWidth || 320
  const a = props.anchor
  let below = 8
  let aboveCandidate = 8
  let preferLeft = 8
  if (a) {
    below = a.top + a.height + 8
    aboveCandidate = a.top - cardH - 8
    preferLeft = a.left
  }
  let nextTop = below
  if (below + cardH > wrapRect.height - 8) {
    nextTop = aboveCandidate >= 8 ? aboveCandidate : Math.max(8, wrapRect.height - cardH - 8)
  }
  let nextLeft = preferLeft
  if (nextLeft + cardW > wrapRect.width - 8) nextLeft = wrapRect.width - cardW - 8
  if (nextLeft < 8) nextLeft = 8
  top.value = nextTop
  left.value = nextLeft
}

function trimmedBody(): string {
  return comment.value.trim()
}

function onSave() {
  const body = trimmedBody()
  if (!body) return
  emit('save', body)
}

function onSendChat() {
  const body = trimmedBody()
  if (!body) return
  emit('send-chat', body)
}

function onComposerEnter() {
  if (canSubmit.value) onSave()
}

function onKeydown(ev: KeyboardEvent) {
  if (!props.open) return
  if (ev.key === 'Escape') {
    ev.preventDefault()
    emit('close')
  }
}

onMounted(() => {
  window.addEventListener('resize', placeCardNear)
  window.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', placeCardNear)
  window.removeEventListener('keydown', onKeydown)
})

defineExpose({ placeCardNear })
</script>

<template>
  <div
    v-show="open"
    ref="cardRef"
    class="rounded-lg comment-pin-inspect-card absolute z-20 flex max-h-[calc(100%-24px)] w-[320px] flex-col border border-line-strong bg-elevated shadow-[var(--shadow-card)]"
    :style="cardStyle"
    data-testid="comment-pin-inspect-card"
    role="dialog"
    :aria-label="t('pages.gateApproval.commentPins.inspectTitle')"
  >
    <div class="flex shrink-0 items-center justify-between gap-2 border-b border-line px-3 py-2.5">
      <div class="min-w-0">
        <div class="truncate font-mono text-[12px] font-semibold text-txt" :title="selector">
          {{ selector || '—' }}
        </div>
        <div v-if="styleRows.length" class="mt-1 flex flex-col gap-0.5" data-testid="comment-pin-style-rows">
          <div
            v-for="row in styleRows"
            :key="row.label"
            class="flex items-center gap-1.5 text-[11px] text-txt3"
          >
            <span class="w-9 shrink-0 text-txt3">{{ row.label }}</span>
            <span
              v-if="row.swatch"
              class="rounded-lg inline-block h-2.5 w-2.5 shrink-0 border border-line"
              :style="{ background: row.swatch }"
            />
            <span class="min-w-0 truncate text-txt2">{{ row.value }}</span>
          </div>
        </div>
      </div>
      <button
        type="button"
        class="rounded-md shrink-0 border border-line px-2 py-1 text-[11px] text-txt2 hover:text-txt"
        data-testid="comment-pin-card-close"
        @click="emit('close')"
      >
        {{ t('common.buttons.close') }}
      </button>
    </div>
    <div class="flex min-h-0 flex-1 flex-col gap-2 overflow-auto px-3 py-2.5">
      <div
        class="rounded-lg flex h-11 items-center justify-center border border-dashed border-line-strong bg-base text-[11px]"
        :class="
          screenshotMissing
            ? 'border-warn/50 text-warn'
            : imageDataUrl
              ? 'border-solid text-accent'
              : 'text-txt3'
        "
        data-testid="comment-pin-thumb"
      >
        <img
          v-if="imageDataUrl && !screenshotMissing"
          :src="imageDataUrl"
          alt=""
          class="h-full max-w-full object-contain"
        />
        <span v-else-if="screenshotMissing">
          {{ t('pages.gateApproval.commentPins.noScreenshot') }}
        </span>
        <span v-else>{{ t('pages.gateApproval.commentPins.shotPreview') }}</span>
      </div>
      <textarea
        v-model="comment"
        class="rounded-md min-h-[56px] w-full resize-y border border-line bg-base px-2.5 py-2 text-[13px] text-txt outline-none focus:border-accent"
        rows="3"
        :placeholder="t('pages.gateApproval.commentPins.placeholder')"
        data-testid="comment-pin-input"
        @keydown.enter.exact.prevent="onComposerEnter"
      />
    </div>
    <div class="flex shrink-0 items-center justify-end gap-1.5 border-t border-line bg-elevated px-3 py-2.5">
      <button
        type="button"
        class="rounded-md border border-line px-3 py-1.5 text-xs font-medium text-txt2 hover:text-txt disabled:cursor-not-allowed disabled:opacity-45"
        :disabled="!canSubmit"
        data-testid="comment-pin-send-chat"
        @click="onSendChat"
      >
        {{ t('pages.gateApproval.commentPins.sendToChat') }}
      </button>
      <button
        type="button"
        class="bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-45"
        :disabled="!canSubmit"
        data-testid="comment-pin-save"
        @click="onSave"
      >
        {{ t('pages.gateApproval.commentPins.savePin') }}
      </button>
    </div>
  </div>
</template>
