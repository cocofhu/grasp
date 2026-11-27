<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, shallowRef, useId, watch, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import WorkspaceChangesDialog from './WorkspaceChangesDialog.vue'
import { workspaceChangesClient, type WorkspaceChangesTarget, type WorkspaceChangesView } from '@/lib/api/clients/workspaceChangesClient'

const props = defineProps<{
  target: WorkspaceChangesTarget
  revision?: string
  workspaceLabels?: Record<string, string>
}>()
const { t, locale } = useI18n()
const cardOpen = ref(false)
const cardId = useId()
const cardAnchor = ref<HTMLElement>()
const card = ref<HTMLElement>()
const cardStyle = ref<CSSProperties>()
const open = ref(false)
const view = shallowRef<WorkspaceChangesView>()
const summaryLoading = ref(false)
const summaryError = ref(false)
const dialogView = shallowRef<WorkspaceChangesView>()
const dialogTarget = shallowRef<WorkspaceChangesTarget>()
const initialRepository = ref<string>()
const loading = ref(false)
const dialogError = ref(false)
const requestedWorkspace = ref<string>()
const switchingWorkspace = computed(() => loading.value && !!requestedWorkspace.value &&
  requestedWorkspace.value !== dialogView.value?.snapshot?.workspace)
let summaryController: AbortController | undefined
let dialogController: AbortController | undefined
const turn = computed(() => !!props.target.turnId)
const ready = computed(() => !!props.target.id)
const repos = computed(() => view.value?.snapshot?.repos ?? [])
const totals = computed(() => repos.value.reduce((total, repo) => ({
  files: total.files + repo.fileCount,
  added: total.added + repo.added,
  deleted: total.deleted + repo.deleted,
}), { files: 0, added: 0, deleted: 0 }))
const firstChanged = computed(() => repos.value.find(repo => repo.files.length)?.files[0]?.path.split('/').at(-1))
const changedRepos = computed(() => repos.value.filter(repo => repo.fileCount > 0))
const workspaceCount = computed(() => view.value?.workspaces?.length ?? 0)
const cardContext = computed(() => {
  const nodeId = view.value?.snapshot?.nodeId
  return nodeId && workspaceCount.value > 1 ? props.workspaceLabels?.[nodeId] || nodeId : ''
})
const cardStatus = computed(() => {
  if (!view.value && summaryLoading.value) return t('common.loading')
  if (repos.value.some(repo => !repo.unavailable)) return t('pages.workspaceChanges.empty')
  return t(view.value?.status === 'pending' ? 'pages.workspaceChanges.pending' : 'pages.workspaceChanges.unavailable')
})
const label = computed(() => t('pages.workspaceChanges.' + (turn.value ? 'turn' : 'changes')))
const number = (value: number) => new Intl.NumberFormat(locale.value).format(value)
const scopeKey = computed(() => targetKey(props.target))

function targetKey(target: WorkspaceChangesTarget) {
  return JSON.stringify([target.kind, target.id, target.nodeId, target.iteration, target.turnId])
}

async function loadSummary() {
  summaryController?.abort()
  if (!ready.value) return
  const controller = new AbortController()
  summaryController = controller
  summaryLoading.value = true
  summaryError.value = false
  try {
    const result = await workspaceChangesClient.list(props.target, undefined, controller.signal)
    if (controller.signal.aborted) return
    view.value = result
  } catch {
    if (!controller.signal.aborted) summaryError.value = true
  } finally {
    if (!controller.signal.aborted) summaryLoading.value = false
  }
}

function placeCard() {
  const anchor = cardAnchor.value
  if (!anchor) return
  const bounds = anchor.getBoundingClientRect()
  const margin = 8
  const gap = 8
  const viewportWidth = document.documentElement.clientWidth
  const width = Math.min(368, viewportWidth - margin * 2)
  const below = window.innerHeight - bounds.bottom - gap - margin
  const above = bounds.top - gap - margin
  if (bounds.bottom <= 0 || bounds.top >= window.innerHeight) {
    closeCard()
    return
  }
  const flip = below < 180 && above > below
  cardStyle.value = {
    width: `${width}px`,
    left: `${Math.round(Math.max(margin, Math.min(bounds.right - width, viewportWidth - width - margin)))}px`,
    maxHeight: `${Math.min(360, Math.max(0, flip ? above : below))}px`,
    ...(flip ? { bottom: `${Math.round(window.innerHeight - bounds.top + gap)}px` } : { top: `${Math.round(bounds.bottom + gap)}px` }),
  }
}

function closeCard(restoreFocus = false) {
  cardOpen.value = false
  if (restoreFocus) cardAnchor.value?.querySelector('button')?.focus({ preventScroll: true })
}

async function toggleCard() {
  if (cardOpen.value) {
    closeCard(true)
    return
  }
  cardOpen.value = true
  placeCard()
  void loadSummary()
  await nextTick()
  if (cardOpen.value) card.value?.focus({ preventScroll: true })
}

function dismissOnOutside(event: Event) {
  const target = event.target
  if (target instanceof Node && !cardAnchor.value?.contains(target) && !card.value?.contains(target)) closeCard()
}

function dismissOnEscape(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  event.preventDefault()
  event.stopPropagation()
  closeCard(true)
}

watch(cardOpen, (visible, _, onCleanup) => {
  if (!visible) return
  document.addEventListener('pointerdown', dismissOnOutside, true)
  document.addEventListener('focusin', dismissOnOutside)
  document.addEventListener('keydown', dismissOnEscape, true)
  window.addEventListener('resize', placeCard)
  window.addEventListener('scroll', placeCard, true)
  onCleanup(() => {
    document.removeEventListener('pointerdown', dismissOnOutside, true)
    document.removeEventListener('focusin', dismissOnOutside)
    document.removeEventListener('keydown', dismissOnEscape, true)
    window.removeEventListener('resize', placeCard)
    window.removeEventListener('scroll', placeCard, true)
  })
}, { flush: 'sync' })

function openDialog(repository?: string) {
  if (cardOpen.value) closeCard(true)
  dialogView.value = view.value
  dialogTarget.value = { ...props.target }
  initialRepository.value = repository
  requestedWorkspace.value = undefined
  dialogError.value = false
  open.value = true
  // A card row refers to the saved snapshot it is displaying.
  if (turn.value || !dialogView.value?.snapshot) void loadDialog()
}

function closeDialog() {
  open.value = false
  dialogController?.abort()
  loading.value = false
}

async function loadDialog(workspace?: string, refresh = false) {
  const target = dialogTarget.value
  if (!target) return
  dialogController?.abort()
  const controller = new AbortController()
  dialogController = controller
  dialogError.value = false
  requestedWorkspace.value = workspace
  loading.value = true
  try {
    const call = refresh ? workspaceChangesClient.refresh : workspaceChangesClient.list
    const result = await call(target, workspace, controller.signal)
    if (controller.signal.aborted) return
    dialogView.value = result
    if (targetKey(target) === scopeKey.value &&
      (!workspace || (refresh && view.value?.snapshot?.workspace === result.snapshot?.workspace))) {
      view.value = result
    }
  } catch {
    if (!controller.signal.aborted) dialogError.value = true
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}

watch(scopeKey, () => {
  summaryController?.abort()
  view.value = undefined
  closeCard()
}, { flush: 'sync' })
watch(() => view.value?.status, status => { if (status === 'no_repositories') closeCard() })
watch(() => [props.target.kind, props.target.id].join('/'), closeDialog, { flush: 'sync' })
watch([scopeKey, () => props.revision], () => {
  if (turn.value || cardOpen.value || open.value) void loadSummary()
}, { immediate: true })
onBeforeUnmount(() => {
  summaryController?.abort()
  dialogController?.abort()
})
</script>

<template>
  <button
    v-if="turn && totals.files > 0"
    type="button"
    class="turn-changes ui-pressable"
    data-testid="workspace-turn-changes"
    :aria-label="`${label} · ${t('pages.workspaceChanges.fileCount', { n: totals.files })}`"
    @click="openDialog()"
  >
    <span class="turn-changes-label"><Icon name="branch" :size="15" />{{ label }}</span>
    <span class="turn-changes-file" v-if="totals.files === 1 && firstChanged">{{ firstChanged }}</span>
    <span v-else class="turn-changes-file">{{ t('pages.workspaceChanges.fileCount', { n: totals.files }) }}</span>
    <span class="change-counts">
      <span class="text-ok">+{{ number(totals.added) }}</span>
      <span class="text-err">−{{ number(totals.deleted) }}</span>
    </span>
    <Icon name="chevron-right" :size="14" class="ml-1 shrink-0 text-txt3" />
  </button>
  <span v-else-if="!turn && view?.status !== 'no_repositories'" ref="cardAnchor" class="inline-flex shrink-0">
    <AppButton
      type="button"
      :variant="cardOpen ? 'subtle' : 'ghost'"
      size="sm"
      icon="branch"
      aria-haspopup="dialog"
      :aria-expanded="cardOpen"
      :aria-controls="cardOpen ? cardId : undefined"
      data-testid="workspace-changes"
      @click="toggleCard"
    >{{ label }}</AppButton>
  </span>

  <Teleport to="body">
    <div
      v-if="cardOpen"
      :id="cardId"
      ref="card"
      class="changes-card scroll-area"
      :style="cardStyle"
      role="dialog"
      :aria-labelledby="cardId + '-title'"
      tabindex="-1"
      data-testid="workspace-changes-card"
    >
      <div class="changes-card-heading">
        <h2 :id="cardId + '-title'">{{ label }}</h2>
        <span v-if="cardContext" class="truncate" :title="cardContext">{{ cardContext }}</span>
      </div>
      <div v-if="summaryError" class="changes-card-feedback">
        <span role="status">{{ t('pages.workspaceChanges.loadError') }}</span>
        <AppButton type="button" size="sm" variant="ghost" @click="loadSummary">{{ t('common.buttons.retry') }}</AppButton>
      </div>
      <button
        v-for="repo in changedRepos"
        :key="repo.directory"
        type="button"
        class="changes-card-row ui-pressable"
        :title="t('pages.workspaceChanges.openDiff')"
        data-testid="workspace-changes-card-open"
        @click="openDialog(repo.directory)"
      >
        <Icon name="branch" :size="16" :interactive="false" class="shrink-0" aria-hidden="true" />
        <span class="changes-card-repo">
          <span class="truncate">{{ repo.name }}</span>
          <span v-if="repo.branch || repo.headSha" class="changes-card-branch truncate" :title="repo.branch">
            {{ repo.branch || t('pages.workspaceChanges.detached') }}
          </span>
        </span>
        <span class="change-counts text-xs" data-testid="workspace-change-stats">
          <span class="text-ok">+{{ number(repo.added) }}</span>
          <span class="text-err">−{{ number(repo.deleted) }}</span>
        </span>
        <Icon name="chevron-right" :size="14" :interactive="false" class="shrink-0 text-txt3" aria-hidden="true" />
        <span class="sr-only">{{ t('pages.workspaceChanges.openDiff') }}</span>
      </button>
      <p v-if="!changedRepos.length && (!summaryError || view)" class="changes-card-feedback" role="status">{{ cardStatus }}</p>
      <button
        v-if="!changedRepos.length && (view?.canRefresh || workspaceCount > 1)"
        type="button"
        class="changes-card-row ui-pressable"
        data-testid="workspace-changes-card-open"
        @click="openDialog()"
      >
        <Icon name="branch" :size="16" :interactive="false" aria-hidden="true" />
        <span class="flex-1">{{ t('pages.workspaceChanges.openDiff') }}</span>
        <Icon name="chevron-right" :size="14" :interactive="false" class="text-txt3" aria-hidden="true" />
      </button>
    </div>
  </Teleport>

  <WorkspaceChangesDialog
    v-if="open && dialogTarget"
    :target="dialogTarget"
    :view="dialogView"
    :loading="loading"
    :workspace-loading="switchingWorkspace"
    :error="dialogError"
    :initial-repository="initialRepository"
    :workspace-labels="workspaceLabels"
    @close="closeDialog"
    @select-workspace="loadDialog($event)"
    @refresh="loadDialog(dialogView?.snapshot?.workspace, true)"
    @retry="loadDialog(requestedWorkspace)"
  />
</template>

<style scoped>
.change-counts { display: inline-flex; align-items: center; gap: 0.4rem; font-variant-numeric: tabular-nums; white-space: nowrap; }
.changes-card {
  position: fixed; z-index: 60; overflow-x: hidden; overflow-y: auto;
  padding: 0.55rem; border: 1px solid rgb(var(--c-line)); border-radius: 13px;
  background: rgb(var(--c-surface)); color: rgb(var(--c-txt)); box-shadow: 0 12px 36px rgb(0 0 0 / 0.14); outline: none;
}
.changes-card-heading {
  display: flex; justify-content: space-between; align-items: baseline; gap: 0.5rem;
  padding: 0.25rem 0.55rem 0.45rem; color: rgb(var(--c-txt3)); font-size: 12px;
}
.changes-card-heading h2 { flex: none; font-weight: 600; color: rgb(var(--c-txt2)); }
.changes-card-row {
  display: flex; align-items: center; gap: 0.55rem; width: 100%; min-height: 44px;
  padding: 0.45rem 0.55rem; border-radius: 8px; text-align: left; color: rgb(var(--c-txt2)); font-size: 13px;
}
.changes-card-row:hover, .changes-card-row:focus-visible { background: rgb(var(--c-elevated)); color: rgb(var(--c-txt)); }
.changes-card-row:focus-visible { outline: 2px solid rgb(var(--c-accent) / 0.5); outline-offset: -2px; }
.changes-card-repo { display: flex; flex-direction: column; flex: 1; min-width: 0; font-weight: 550; }
.changes-card-branch { color: rgb(var(--c-txt3)); font-size: 11px; font-weight: 400; }
.changes-card-feedback {
  display: flex; align-items: center; justify-content: space-between; gap: 0.5rem;
  padding: 0.45rem 0.55rem; font-size: 12px; color: rgb(var(--c-txt3));
}
.turn-changes {
  display: flex; width: 100%; min-height: 36px; align-items: center; gap: 0.6rem; margin-bottom: 0.55rem;
  padding: 0.35rem 0.65rem; border: 1px solid rgb(var(--c-line)); border-radius: 9px;
  color: rgb(var(--c-txt2)); background: rgb(var(--c-surface)); text-align: left; font-size: 12px;
}
.turn-changes:hover { background: rgb(var(--c-elevated)); }
.turn-changes:focus-visible { outline: 2px solid rgb(var(--c-accent)); outline-offset: 2px; }
.turn-changes-label { display: inline-flex; align-items: center; gap: 0.35rem; flex: none; font-weight: 600; color: rgb(var(--c-txt)); }
.turn-changes-file { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
