<script setup lang="ts">
import { computed, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import { LIVE_CARD_HOST, isLiveBusy, isLiveOpen, type LiveCmd } from '@/lib/inbox/liveVariants'

/**
 * Chat card for a Live variant request. State comes from the host's Live
 * store (the preview drawer). `compact` keeps only the request headline;
 * `agent` places the card on the agent side, under the reply.
 */
const props = defineProps<{
  liveRef: { sid: string; op: string; variant?: number; prompt?: string }
  compact?: boolean
  agent?: boolean
}>()

/** Server summary for page-scope sessions (engine/live.go). */
const PAGE_SUMMARY = '页面候选'

const { t } = useI18n()
const host = inject(LIVE_CARD_HOST, null)

const session = computed(() => host?.store.sessions[props.liveRef.sid] ?? null)
const view = computed(() => host?.store.views[props.liveRef.sid] ?? null)
const variants = computed(() => session.value?.variants ?? [])
const state = computed(() => session.value?.state ?? '')
const opKey = computed(() => (['generate', 'insert', 'steer', 'refine', 'accept', 'discard', 'mount_failed'].includes(props.liveRef.op) ? props.liveRef.op : 'generate'))
const stateKey = computed(() => (state.value ? state.value : 'unknown'))
/** `p「Sign in」` reads as markup; the quoted text alone names the element. */
const target = computed(() => {
  const s = session.value?.summary || ''
  return s === PAGE_SUMMARY ? '' : s.replace(/^[\w-]+(?=「)/, '')
})
const headline = computed(() => {
  const op = opKey.value
  const n = props.liveRef.variant
  if (op === 'generate' || op === 'insert') {
    const count = session.value?.count || variants.value.length || 3
    return t(`pages.embedChat.live.heads.${op}`, { n: count, target: target.value || t('pages.embedChat.live.heads.page') })
  }
  if (op === 'refine') return n ? t('pages.embedChat.live.heads.refine', { n }) : t('pages.embedChat.live.heads.refineMore')
  if (op === 'accept') {
    const label = variants.value.find((v) => v.n === n)?.label
    return t('pages.embedChat.live.heads.accept', { n: n ?? '' }) + (label ? ` · ${label}` : '')
  }
  return t(`pages.embedChat.live.heads.${op}`)
})
/** Generate and insert name the element in the headline already. */
const showTarget = computed(() => !!target.value && opKey.value !== 'generate' && opKey.value !== 'insert')
/** "Ready" is implied by the controls below. */
const showState = computed(() => !!session.value && !!state.value && state.value !== 'ready')
const tone = computed(() => {
  if (state.value === 'failed') return 'bg-err'
  if (isLiveBusy(state.value)) return 'bg-warn animate-pulse'
  if (state.value === 'ready') return 'bg-ok'
  return 'bg-txt3'
})
const current = computed(() => view.value?.current ?? 0)
const currentIndex = computed(() => variants.value.findIndex((v) => v.n === current.value))
/** Replace sessions keep the original in the page wrapper; insert sessions have none. */
const hasOriginal = computed(() => session.value?.mode === 'replace')
const onOriginal = computed(() => hasOriginal.value && !!view.value?.original)
/** Switching works on the original too; adopting needs a candidate. */
const canNav = computed(() => !!host?.interactive && state.value === 'ready' && (currentIndex.value >= 0 || onOriginal.value))
const canAct = computed(() => !!host?.interactive && state.value === 'ready' && currentIndex.value >= 0)
const canDiscard = computed(() => !!host?.interactive && isLiveOpen(state.value) && !isLiveBusy(state.value))
const failedSteer = computed(() => session.value?.mode === 'steer' && state.value === 'failed')
const canRetryAccept = computed(() => !!host?.interactive && state.value === 'failed' && session.value?.retryAccept === true && Number.isInteger(session.value.selected) && (session.value.selected ?? 0) > 0)
/** Only the newest turn of a session carries controls. */
const isLatestTurn = computed(() => !props.compact && props.liveRef.op !== 'accept' && props.liveRef.op !== 'discard')

function send(cmd: LiveCmd, variant?: number) {
  host?.command(props.liveRef.sid, cmd, variant)
}

function step(delta: number) {
  if (!variants.value.length) return
  const list = [...(hasOriginal.value ? [0] : []), ...variants.value.map((v) => v.n)]
  const i = Math.max(0, list.indexOf(onOriginal.value ? 0 : current.value))
  send('goto', list[(i + delta + list.length) % list.length])
}
</script>

<template>
  <div
    class="w-full max-w-[340px] rounded-lg border border-accent/30 bg-accent-dim/40 px-3 py-2 text-left"
    :class="agent ? 'mt-2' : 'mb-1.5 ml-auto'"
    data-testid="live-variant-card"
    :data-state="stateKey"
    :data-side="agent ? 'agent' : 'human'"
  >
    <div class="flex items-center gap-1.5 text-[12px] font-medium text-txt">
      <Icon name="sparkles" :size="12" class="shrink-0 text-accent" aria-hidden="true" />
      <span class="min-w-0 truncate" :title="session?.selector || headline" data-testid="live-variant-headline">{{ headline }}</span>
      <span v-if="showState" class="ml-auto inline-flex shrink-0 items-center gap-1 text-[11px] font-normal text-txt3" role="status" data-testid="live-variant-state">
        <span class="h-1.5 w-1.5 rounded-full" :class="tone" aria-hidden="true" />
        {{ t(`pages.embedChat.live.states.${stateKey}`) }}
      </span>
    </div>
    <p v-if="liveRef.prompt && !agent" class="m-0 mt-1 whitespace-pre-wrap break-words text-[12px] leading-snug text-txt2" data-testid="live-variant-prompt">{{ liveRef.prompt }}</p>
    <p v-if="showTarget" class="m-0 mt-1 truncate text-[11px] text-txt3" :title="session?.selector">{{ target }}</p>
    <p v-if="session?.state === 'failed' && session.error" class="m-0 mt-1 text-[11px] leading-snug text-err" data-testid="live-variant-error">
      {{ t('pages.embedChat.live.failed', { error: session.error }) }}
    </p>
    <p v-else-if="session?.state === 'accepted' && session.selected && opKey !== 'accept'" class="m-0 mt-1 text-[11px] text-txt3">
      {{ t('pages.embedChat.live.selected', { n: session.selected }) }}
    </p>
    <p v-if="failedSteer" class="m-0 mt-1 text-[11px] leading-snug text-txt3" data-testid="live-steer-partial">
      {{ t('pages.embedChat.live.steerPartial') }}
    </p>
    <button v-if="canRetryAccept" type="button" class="mt-2 rounded bg-accent px-2 py-0.5 text-[11px] font-medium text-white" data-testid="live-variant-retry-accept" @click="send('retry-accept')">
      {{ t('pages.embedChat.live.retryAccept') }}
    </button>

    <template v-if="session && isLatestTurn && variants.length && isLiveOpen(state)">
      <div class="mt-2 flex flex-wrap gap-1" role="group" :aria-label="t('pages.embedChat.live.variantCount', { n: variants.length })">
        <button
          v-if="hasOriginal"
          type="button"
          class="rounded border px-1.5 py-0.5 text-[11px] transition-colors disabled:cursor-default"
          :class="onOriginal ? 'border-accent bg-accent/15 text-txt' : 'border-dashed border-line bg-surface/70 text-txt2 hover:border-accent/50'"
          :aria-pressed="onOriginal"
          :title="t('pages.embedChat.live.gotoOriginal')"
          :disabled="!canNav"
          data-testid="live-variant-original"
          @click="send('goto', 0)"
        >
          {{ t('pages.embedChat.live.original') }}
        </button>
        <button
          v-for="v in variants"
          :key="v.n"
          type="button"
          class="rounded border px-1.5 py-0.5 text-[11px] transition-colors disabled:cursor-default"
          :class="v.n === current ? 'border-accent bg-accent/15 text-txt' : 'border-line bg-surface/70 text-txt2 hover:border-accent/50'"
          :aria-pressed="v.n === current"
          :aria-label="t('pages.embedChat.live.goto', { n: v.n })"
          :disabled="!canNav"
          data-testid="live-variant-chip"
          @click="send('goto', v.n)"
        >
          {{ v.n }}<template v-if="v.label"> · {{ v.label }}</template>
        </button>
      </div>
      <div v-if="host?.interactive" class="mt-2 flex flex-wrap items-center gap-1.5 text-[11px]">
        <button type="button" class="rounded border border-line px-1.5 py-0.5 text-txt2 hover:text-txt disabled:opacity-40" :disabled="!canNav" :aria-label="t('pages.embedChat.live.prev')" data-testid="live-variant-prev" @click="step(-1)">‹</button>
        <span class="tabular-nums text-txt3" data-testid="live-variant-viewing">{{
          onOriginal ? t('pages.embedChat.live.viewingOriginal') : t('pages.embedChat.live.viewing', { current: currentIndex + 1, total: variants.length })
        }}</span>
        <button type="button" class="rounded border border-line px-1.5 py-0.5 text-txt2 hover:text-txt disabled:opacity-40" :disabled="!canNav" :aria-label="t('pages.embedChat.live.next')" data-testid="live-variant-next" @click="step(1)">›</button>
        <button
          type="button"
          class="rounded border border-line px-1.5 py-0.5 text-txt2 hover:text-txt disabled:opacity-40"
          :disabled="!canNav"
          data-testid="live-variant-mode"
          @click="send(view?.mode === 'compare' ? 'inplace' : 'compare')"
        >
          {{ view?.mode === 'compare' ? t('pages.embedChat.live.inplace') : t('pages.embedChat.live.compare') }}
        </button>
        <span class="ml-auto flex gap-1.5">
          <button type="button" class="rounded border border-line px-2 py-0.5 text-txt2 hover:text-err disabled:opacity-40" :disabled="!canDiscard" data-testid="live-variant-discard" @click="send('discard')">
            {{ t('pages.embedChat.live.discard') }}
          </button>
          <button type="button" class="rounded bg-accent px-2 py-0.5 font-medium text-white disabled:opacity-40" :disabled="!canAct" data-testid="live-variant-accept" @click="send('accept', current)">
            {{ t('pages.embedChat.live.accept') }}
          </button>
        </span>
      </div>
      <p v-else class="m-0 mt-1.5 text-[11px] text-txt3">{{ t('pages.embedChat.live.openInPreview') }}</p>
    </template>
    <div v-if="session && isLatestTurn && isLiveOpen(state) && !variants.length" class="mt-2 flex flex-wrap items-center gap-1.5 text-[11px]">
      <template v-if="host?.interactive">
        <button v-if="failedSteer" type="button" class="rounded border border-line px-2 py-0.5 text-txt2 hover:text-txt disabled:opacity-40" :disabled="!canDiscard" data-testid="live-variant-retry" @click="send('retry')">
          {{ t('pages.embedChat.live.retry') }}
        </button>
        <button type="button" class="rounded border border-line px-2 py-0.5 text-txt2 hover:text-err disabled:opacity-40" :disabled="!canDiscard" data-testid="live-variant-discard" @click="send('discard')">
          {{ failedSteer ? t('pages.embedChat.live.dismissSteer') : t('pages.embedChat.live.discard') }}
        </button>
      </template>
      <p v-else class="m-0 text-txt3">{{ t('pages.embedChat.live.openInPreview') }}</p>
    </div>
  </div>
</template>
