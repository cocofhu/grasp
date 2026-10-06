<script setup lang="ts">
import { ref, watch, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import ParagraphInput from '@/components/ui/ParagraphInput.vue'
import HardLoadLayer from '@/components/run/HardLoadLayer.vue'
import { isAbortError } from '@/lib/run/liveLogRehydrate'
import ReposEditor, { type RepoRow } from '@/components/ui/ReposEditor.vue'
import PrioritySegmented, { type RunPriority } from '@/components/ui/PrioritySegmented.vue'
import { api } from '@/lib/api/api'
import { isCompositeFilled, normalizeCompositeSubmit } from '@/lib/shared/compositeText'
import { MAX_RUN_TAGS, MAX_TAG_RUNES, validateRunTag } from '@/lib/run/runTags'
import {
  type RunSandboxEnvEntry,
  validateRunSandboxEnvRows,
} from '@/lib/run/runSandboxEnv'
import type { ClarifyImage } from '@/lib/shared/types'

export type InputField = {
  key: string
  desc?: string
  type?: string
  required?: boolean
  default?: string
  editable?: boolean
  options?: string
}

type Phase = 'form' | 'loading' | 'success' | 'error'

const props = defineProps<{
  open: boolean
  workflowId: string
  projectId?: string
  workflowName: string
  fields: InputField[]
  runInputs: Record<string, string>
  runImages: Record<string, ClarifyImage[]>
  hintExtra?: string
  beforeStart?: () => Promise<void>
  draftRestored?: boolean
  /** Optional run title override (home chat first message). */
  runTitle?: string
  /**
   * Opening chat message for an approve-first workflow (home chat). The engine
   * delivers it into the approve node's sandbox once that node parks.
   */
  firstMessage?: { text: string; images?: ClarifyImage[] } | null
  /**
   * Prefill segmented priority when the modal opens (home Composer path).
   * Unset / invalid values still default to normal (plan g2.2).
   */
  initialPriority?: RunPriority
  /**
   * Home page only. Editor, project detail, and sidebar quick launch leave this
   * unset so the run still executes the latest saved head.
   */
  publishedSnapshot?: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'stayed'): void
  (e: 'view-run', runId: string): void
  (e: 'update:loading', value: boolean): void
  (e: 'save-draft'): void
  (e: 'started', runId: string): void
}>()

const { t } = useI18n()
const phase = ref<Phase>('form')
const startError = ref('')
const successRunId = ref('')
const priority = ref<RunPriority>('normal')
const checkIconEl = ref<HTMLElement | null>(null)
const modalRef = ref<InstanceType<typeof AppModal> | null>(null)

const loading = computed(() => phase.value === 'loading')
const isForm = computed(() => phase.value === 'form' || phase.value === 'error')
const isLoading = computed(() => phase.value === 'loading')
const isSuccess = computed(() => phase.value === 'success')
const bodyOverflow = computed<'auto' | 'hidden'>(() =>
  phase.value === 'loading' || phase.value === 'success' ? 'hidden' : 'auto',
)
const scrollAreaMinHeight = ref<number | undefined>(undefined)
const tags = ref<string[]>([])
const tagInput = ref('')
const tagSuggestions = ref<string[]>([])
const maxTags = MAX_RUN_TAGS
const maxTagRunes = MAX_TAG_RUNES
const reposDraft = ref<Record<string, RepoRow[]>>({})
const envRows = ref<RunSandboxEnvEntry[]>([])
let startAbort: AbortController | null = null
let startGen = 0
/** loading 可见层显式高度：≥ max(捕获高度, 200)，捕获为空也至少 200px */
const loadingLayerMinHeight = computed(() =>
  isLoading.value ? Math.max(scrollAreaMinHeight.value ?? 0, 200) : undefined,
)

function captureScrollAreaHeight() {
  const el = modalRef.value?.scrollAreaEl
  if (el && el.clientHeight > 0) scrollAreaMinHeight.value = el.clientHeight
}

function resetScrollTop() {
  nextTick(() => {
    const el = modalRef.value?.scrollAreaEl
    if (el) el.scrollTop = 0
  })
}

watch(() => phase.value, (p) => {
  resetScrollTop()
  if (p === 'form' || p === 'error') scrollAreaMinHeight.value = undefined
})

watch(
  () => props.open,
  (open) => {
    if (open) {
      phase.value = 'form'
      startError.value = ''
      successRunId.value = ''
      const seed = props.initialPriority
      priority.value = seed === 'high' || seed === 'low' || seed === 'normal' ? seed : 'normal'
      scrollAreaMinHeight.value = undefined
      reposDraft.value = {}
      tags.value = []
      tagInput.value = ''
      tagSuggestions.value = []
      envRows.value = []
      emit('update:loading', false)
      resetScrollTop()
      if (props.projectId) {
        void api.listProjectRunTags(props.projectId).then((res) => {
          tagSuggestions.value = Array.isArray(res.tags) ? res.tags : []
        }).catch(() => {
          tagSuggestions.value = []
        })
      }
    }
  },
  // immediate: v-if + open=true 挂载时（ProjectDetail/WorkflowList）也要拉取存量 tags
  { immediate: true },
)

watch(loading, (v) => emit('update:loading', v))

watch(isSuccess, async (success) => {
  if (!success) return
  await nextTick()
  const el = checkIconEl.value
  if (!el) return
  el.classList.remove('check-pop')
  void el.offsetWidth
  el.classList.add('check-pop')
})

function onEnterKeydown(e: KeyboardEvent) {
  if (!props.open || phase.value !== 'success') return
  if (e.key === 'Enter') {
    e.preventDefault()
    viewRun()
  }
}

onMounted(() => {
  document.addEventListener('keydown', onEnterKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('keydown', onEnterKeydown)
  startAbort?.abort()
  startAbort = null
  startGen++
})

function fieldOptions(f: InputField): string[] {
  return String(f.options || '')
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function closeFromForm() {
  emit('close')
}

function closeSuccessAsStayed() {
  emit('stayed')
  emit('close')
}

function onModalClose() {
  if (phase.value === 'success') {
    closeSuccessAsStayed()
    return
  }
  if (phase.value === 'loading') {
    startAbort?.abort()
    startAbort = null
    startGen++
    emit('update:loading', false)
    emit('close')
    return
  }
  closeFromForm()
}

function viewRun() {
  const id = successRunId.value
  emit('view-run', id)
  emit('close')
}

function bodyLayerClass(visible: boolean) {
  if (visible) return 'layer-visible'
  if (isSuccess.value) return 'layer-hidden-success'
  return 'layer-hidden'
}

function fieldPlaceholder(f: InputField, paragraph = false): string {
  if (f.editable === false) return t('pages.runLaunch.lockedDefault')
  if (f.type === 'number') return t('pages.runLaunch.inputNumber')
  if (paragraph) return t('pages.runLaunch.inputParagraph', { key: f.key })
  return t('pages.runLaunch.inputPlaceholder', { key: f.key })
}

// reposFor lazily parses a repos field's JSON string value into editable rows,
// caching the array so v-model edits stay reactive. syncRepos writes it back.
function reposFor(key: string): RepoRow[] {
  if (!reposDraft.value[key]) {
    let arr: any[] = []
    const raw = props.runInputs[key]
    if (Array.isArray(raw)) arr = raw as any[]
    else if (typeof raw === 'string' && raw.trim()) {
      try {
        const p = JSON.parse(raw)
        if (Array.isArray(p)) arr = p
      } catch {
        arr = []
      }
    }
    reposDraft.value[key] = arr.map((r) => ({
      name: r?.name ?? '',
      url: r?.url ?? '',
      branch: r?.branch ?? '',
    }))
  }
  return reposDraft.value[key]
}
function syncRepos(key: string) {
  ;(props.runInputs as Record<string, any>)[key] = JSON.stringify(reposFor(key))
}
function onReposUpdate(key: string, rows: RepoRow[]) {
  reposDraft.value[key] = rows
  syncRepos(key)
}

function boolIsOn(key: string): boolean {
  return props.runInputs[key] === 'true'
}

function boolDisplayValue(key: string): 'true' | 'false' {
  return boolIsOn(key) ? 'true' : 'false'
}

function setBoolField(f: InputField, on: boolean) {
  if (f.editable === false) return
  props.runInputs[f.key] = on ? 'true' : 'false'
}

function validateTag(value: string): string {
  const code = validateRunTag(value)
  if (!code || code === 'empty') return ''
  if (code === 'too_long') return t('common.tagFilter.tagTooLong', { max: maxTagRunes })
  return t('common.tagFilter.tagInvalid')
}

const tagError = computed(() => validateTag(tagInput.value))
const filteredTagSuggestions = computed(() => {
  if (!props.projectId || !tagInput.value.trim()) return []
  return tagSuggestions.value
    .filter((tag) => tag.includes(tagInput.value.trim()) && !tags.value.includes(tag))
    .slice(0, 8)
})

function addTag(raw: string) {
  const trimmed = raw.trim()
  const err = validateTag(trimmed)
  if (!trimmed || err || tags.value.includes(trimmed) || tags.value.length >= maxTags) return
  tags.value = [...tags.value, trimmed]
  tagInput.value = ''
}

function removeTag(tag: string) {
  tags.value = tags.value.filter((item) => item !== tag)
}

function addEnvRow() {
  envRows.value = [...envRows.value, { key: '', value: '', secret: false }]
}

function removeEnvRow(i: number) {
  envRows.value = envRows.value.filter((_, idx) => idx !== i)
}

function setEnvSecret(i: number, secret: boolean) {
  const row = envRows.value[i]
  if (!row) return
  envRows.value[i] = { ...row, secret }
}

function onTagKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' || e.key === ',') {
    e.preventDefault()
    addTag(tagInput.value)
  } else if (e.key === 'Backspace' && !tagInput.value && tags.value.length) {
    tags.value = tags.value.slice(0, -1)
  }
}

async function startRun() {
  const missing = props.fields.find((f) => {
    if (!f.required) return false
    if (f.type === 'paragraph') {
      return !isCompositeFilled({ text: props.runInputs[f.key] ?? '', images: props.runImages[f.key] ?? [] })
    }
    return !String(props.runInputs[f.key] ?? '').trim()
  })
  if (missing) {
    phase.value = 'error'
    startError.value = t('pages.runLaunch.requiredMissing', { field: missing.desc || missing.key })
    return
  }

  const { entries: envEntries, problems: envProblems } = validateRunSandboxEnvRows(envRows.value)
  if (envProblems.length) {
    phase.value = 'error'
    startError.value = t('pages.runLaunch.envInvalid', { keys: envProblems.join(', ') })
    return
  }

  captureScrollAreaHeight()
  startAbort?.abort()
  const gen = ++startGen
  startAbort = new AbortController()
  phase.value = 'loading'
  startError.value = ''
  emit('update:loading', true)

  const inputs: Record<string, any> = {}
  for (const f of props.fields) {
    if (f.type === 'paragraph') {
      inputs[f.key] = normalizeCompositeSubmit(props.runInputs[f.key] ?? '', props.runImages[f.key] ?? [])
    } else {
      inputs[f.key] = props.runInputs[f.key]
    }
  }

  try {
    if (props.beforeStart) await props.beforeStart()
    const res = await api.startRun(props.workflowId, inputs, 'manual', priority.value, tags.value, {
      signal: startAbort.signal,
      env: envEntries.length ? envEntries : undefined,
      title: props.runTitle?.trim() || undefined,
      firstMessage: props.firstMessage ?? undefined,
      ...(props.publishedSnapshot ? { publishedSnapshot: true } : {}),
    })
    if (gen !== startGen) return
    successRunId.value = res.id
    emit('started', res.id)
    phase.value = 'success'
  } catch (e: any) {
    if (gen !== startGen || isAbortError(e) || startAbort.signal.aborted) return
    phase.value = 'error'
    startError.value = String(e?.message || e)
  } finally {
    if (gen === startGen) emit('update:loading', false)
  }
}
</script>

<template>
  <AppModal
    ref="modalRef"
    :open="open"
    :body-overflow="bodyOverflow"
    :body-min-height="isLoading || isSuccess ? scrollAreaMinHeight : undefined"
    @close="onModalClose"
  >
    <template #header>
      <div class="header-stack relative min-w-0 flex-1 self-stretch overflow-hidden">
        <div
          class="header-layer text-[15px] font-semibold text-txt"
          :class="isSuccess ? 'layer-hidden' : 'layer-visible'"
        >
          <span class="truncate">{{ t('pages.runLaunch.title', { name: workflowName }) }}</span>
        </div>
        <div
          class="header-layer flex items-center gap-2.5 text-[15px] font-semibold text-ok"
          :class="isSuccess ? 'layer-visible' : 'layer-hidden'"
        >
          <span
            ref="checkIconEl"
            class="rounded-full check-icon-wrap flex h-7 w-7 shrink-0 items-center justify-center border border-ok/30 bg-ok/10 text-ok"
          >
            <Icon name="check" :size="16" />
          </span>
          <span>{{ t('pages.runLaunch.started') }}</span>
        </div>
      </div>
    </template>

    <div class="body-stack relative" :class="isSuccess ? 'min-h-0' : 'min-h-[140px]'">
      <div class="body-layer space-y-4" :class="bodyLayerClass(isForm)">
        <div
          v-if="draftRestored"
          class="flex items-center gap-2 rounded-md border border-info/30 bg-info/10 px-3 py-2 text-[12px] text-info"
        >
          <Icon name="clock" :size="14" class="shrink-0" />
          {{ t('pages.runLaunch.draftRestored') }}
        </div>
        <p v-if="!fields.length" class="text-[12px] text-txt3">
          {{ hintExtra ? t('pages.runLaunch.noFieldsWithHint') : t('pages.runLaunch.noFields') }}
        </p>
        <div v-for="f in fields" :key="f.key">
          <label class="label flex items-center gap-1.5">
            <span>{{ f.desc || f.key }}</span>
            <span v-if="f.required" class="text-err">*</span>
            <span class="font-mono text-txt3">{{ f.key }}</span>
            <span v-if="f.editable === false" class="chip text-txt3"><Icon name="gate" :size="11" />{{ t('pages.runLaunch.locked') }}</span>
          </label>
          <select v-if="f.type === 'select'" v-model="runInputs[f.key]" :disabled="f.editable === false" class="input disabled:opacity-60">
            <option v-for="o in fieldOptions(f)" :key="o" :value="o">{{ o }}</option>
          </select>
          <div
            v-else-if="f.type === 'bool'"
            class="flex w-fit select-none items-center gap-2.5"
            :class="{ 'opacity-60': f.editable === false }"
          >
            <AppSwitch
              :model-value="boolIsOn(f.key)"
              :disabled="f.editable === false"
              :aria-label="f.desc || f.key"
              @update:model-value="setBoolField(f, $event)"
            />
            <span class="font-mono text-[12px] text-txt2">{{ boolDisplayValue(f.key) }}</span>
          </div>
          <input
            v-else-if="f.type === 'number'"
            v-model="runInputs[f.key]"
            type="number"
            :disabled="f.editable === false"
            class="input disabled:opacity-60"
            :placeholder="f.editable === false ? t('pages.runLaunch.lockedNumber') : t('pages.runLaunch.inputNumber')"
          />
          <input
            v-else-if="f.type === 'text'"
            v-model="runInputs[f.key]"
            :disabled="f.editable === false"
            class="input disabled:opacity-60"
            :placeholder="fieldPlaceholder(f)"
          />
          <ParagraphInput
            v-else-if="f.type === 'paragraph'"
            v-model:text="runInputs[f.key]"
            v-model:images="runImages[f.key]"
            :disabled="f.editable === false"
            :placeholder="fieldPlaceholder(f, true)"
          />
          <ReposEditor
            v-else-if="f.type === 'repos'"
            :repos="reposFor(f.key)"
            :editable="f.editable !== false"
            i18n-prefix="pages.runLaunch.repos"
            @update:repos="onReposUpdate(f.key, $event)"
          />
          <textarea
            v-else
            v-model="runInputs[f.key]"
            :disabled="f.editable === false"
            class="input min-h-[72px] disabled:opacity-60"
            :placeholder="fieldPlaceholder(f)"
          />
        </div>
        <div class="space-y-2">
          <div class="flex items-center justify-between gap-2">
            <label class="block text-[12px] font-medium text-txt2">{{ t('pages.runLaunch.envLabel') }}</label>
            <AppButton variant="ghost" class="!px-2 !py-1 text-[12px]" @click="addEnvRow">
              {{ t('pages.runLaunch.envAdd') }}
            </AppButton>
          </div>
          <p class="text-[11px] leading-relaxed text-txt3">{{ t('pages.runLaunch.envHint') }}</p>
          <div v-if="envRows.length" class="rounded-lg overflow-hidden border border-line bg-base">
            <div class="hidden gap-2 border-b border-line bg-elevated/55 px-2 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-txt3 sm:grid sm:grid-cols-[minmax(0,1.1fr)_minmax(0,1.4fr)_72px_36px]">
              <span>{{ t('pages.runLaunch.envKey') }}</span>
              <span>{{ t('pages.runLaunch.envValue') }}</span>
              <span>{{ t('pages.runLaunch.envSecret') }}</span>
              <span />
            </div>
            <div
              v-for="(row, i) in envRows"
              :key="i"
              class="grid grid-cols-1 gap-2 border-b border-line px-2 py-2 last:border-b-0 sm:grid-cols-[minmax(0,1.1fr)_minmax(0,1.4fr)_72px_36px] sm:items-center"
              data-testid="run-launch-env-row"
            >
              <input
                v-model="row.key"
                class="input px-2.5 py-1.5 font-mono text-xs"
                :placeholder="t('pages.runLaunch.envKey')"
              />
              <input
                v-model="row.value"
                class="input min-w-0 px-2.5 py-1.5 text-xs"
                :type="row.secret ? 'password' : 'text'"
                :placeholder="t('pages.runLaunch.envValue')"
              />
              <button
                type="button"
                class="chip w-full justify-center"
                :class="row.secret ? 'border-accent/50 text-accent-2' : 'text-txt3'"
                @click="setEnvSecret(i, !row.secret)"
              >
                {{ row.secret ? t('pages.runLaunch.envSecretOn') : t('pages.runLaunch.envSecretOff') }}
              </button>
              <button
                type="button"
                class="chip justify-center text-txt3"
                :aria-label="t('pages.runLaunch.envRemove')"
                @click="removeEnvRow(i)"
              >
                <Icon name="close" :size="12" />
              </button>
            </div>
          </div>
          <p v-else class="text-[11px] text-txt3">{{ t('pages.runLaunch.envEmpty') }}</p>
        </div>
        <div v-if="startError" class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5" />{{ startError }}
        </div>
        <div class="space-y-1.5">
          <label class="block text-[12px] font-medium text-txt2">{{ t('pages.runLaunch.priorityLabel') }}</label>
          <PrioritySegmented v-model="priority" />
          <p class="text-[11px] leading-relaxed text-txt3">{{ t('pages.runLaunch.priorityHint') }}</p>
        </div>
        <div class="space-y-2">
          <div class="flex items-center justify-between gap-2">
            <label class="block text-[12px] font-medium text-txt2">{{ t('pages.runLaunch.tagsLabel') }}</label>
            <span class="text-[11px] text-txt3">{{ tags.length }}/{{ maxTags }}</span>
          </div>
          <p class="text-[11px] leading-relaxed text-txt3">{{ t('pages.runLaunch.tagsHint') }}</p>
          <div class="rounded-md border border-line bg-base px-2 py-2">
            <div v-if="tags.length" class="mb-2 flex flex-wrap gap-1.5">
              <button
                v-for="tag in tags"
                :key="tag"
                type="button"
                class="chip inline-flex items-center gap-1"
                @click="removeTag(tag)"
              >
                <span>{{ tag }}</span>
                <Icon name="close" :size="11" />
              </button>
            </div>
            <input
              v-model="tagInput"
              class="w-full bg-transparent text-sm outline-none"
              :placeholder="t('pages.runLaunch.tagsPlaceholder')"
              :disabled="tags.length >= maxTags"
              @keydown="onTagKeydown"
            />
          </div>
          <div v-if="tagError" class="text-[11px] text-err">{{ tagError }}</div>
          <div v-else-if="tags.length >= maxTags" class="text-[11px] text-txt3">{{ t('pages.runLaunch.tagsLimit', { max: maxTags }) }}</div>
          <div v-if="filteredTagSuggestions.length" class="flex flex-wrap gap-1.5">
            <button
              v-for="tag in filteredTagSuggestions"
              :key="tag"
              type="button"
              class="chip text-txt2 hover:text-txt"
              @click="addTag(tag)"
            >{{ tag }}</button>
          </div>
        </div>
        <div class="flex items-center gap-2 text-[12px] text-txt3">
          <Icon name="trigger" :size="13" />{{ t('pages.runLaunch.triggerManual') }}{{ hintExtra || '' }}
        </div>
      </div>

      <div
        class="body-layer flex flex-col justify-center"
        :class="bodyLayerClass(isLoading)"
        :style="loadingLayerMinHeight != null ? { minHeight: `${loadingLayerMinHeight}px` } : undefined"
      >
        <HardLoadLayer
          :overlay="false"
          :stuck-after-ms="90_000"
          :stage="t('common.buttons.starting')"
          @retry="startRun"
        />
      </div>

      <div
        class="body-layer flex flex-col items-center justify-center px-4 py-7 text-center"
        :class="isSuccess ? 'layer-visible' : 'layer-hidden'"
      >
        <div class="mb-2 text-[11px] font-medium uppercase tracking-wider text-txt3">{{ t('pages.runLaunch.startedLabel') }}</div>
        <div class="max-w-full break-all [overflow-wrap:anywhere] text-[18px] font-semibold text-txt">{{ workflowName }}</div>
        <div class="mt-3 text-[11px] text-txt3">
          {{ t('pages.runLaunch.enterHint') }}
        </div>
      </div>
    </div>

    <template #footer>
      <template v-if="phase === 'success'">
        <AppButton variant="ghost" @click="viewRun">{{ t('common.buttons.viewRun') }}</AppButton>
        <AppButton variant="primary" @click="closeSuccessAsStayed">{{ t('common.buttons.stayOnPage') }}</AppButton>
      </template>
      <template v-else-if="phase === 'loading'">
        <AppButton variant="primary" disabled>{{ t('common.buttons.starting') }}</AppButton>
      </template>
      <template v-else>
        <AppButton variant="ghost" icon="save" class="mr-auto" @click="emit('save-draft')">{{ t('common.buttons.saveRunDraft') }}</AppButton>
        <AppButton variant="ghost" @click="closeFromForm">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="primary" icon="play" @click="startRun">{{ t('common.buttons.startRun') }}</AppButton>
      </template>
    </template>
  </AppModal>
</template>

<style scoped>
.header-stack {
  margin: 0 -4px;
  min-height: 56px;
}

.header-layer {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  padding-right: 36px;
  transition:
    opacity var(--dur-overlay) var(--ease-out-expo),
    transform var(--dur-overlay) var(--ease-out-expo);
}

.body-layer {
  transition:
    opacity var(--dur-overlay) var(--ease-out-expo),
    transform var(--dur-overlay) var(--ease-out-expo);
}

.layer-hidden {
  opacity: 0;
  transform: translateY(8px);
  pointer-events: none;
  position: absolute;
  inset: 0;
}

.layer-hidden-success {
  display: none;
}

.header-layer.layer-hidden {
  transform: translateY(-6px);
}

.layer-visible {
  opacity: 1;
  transform: translateY(0);
}

.check-icon-wrap {
  opacity: 0;
  transform: scale(0.82);
}

.check-icon-wrap.check-pop {
  animation: checkPop var(--dur-overlay) var(--ease-out-expo) forwards;
}

@keyframes checkPop {
  to {
    opacity: 1;
    transform: scale(1);
  }
}
</style>
