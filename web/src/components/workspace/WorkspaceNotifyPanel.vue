<script setup lang="ts">
import SlidingTabs from '@/components/ui/SlidingTabs.vue'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import type { Workspace, WorkspaceNotifyPolicy } from '@/lib/shared/types'
import {
  RUN_NOTIFY_PLACEHOLDERS,
  defaultEditableRunNotifyTemplate,
  renderRunNotifyMessage,
  type RunNotifyKind,
} from '@/lib/run/runNotifyTemplate'
import AppButton from '@/components/ui/AppButton.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import AppInlineError from '@/components/ui/AppInlineError.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'

const props = defineProps<{
  workspace: Workspace
}>()

const emit = defineEmits<{
  updated: [workspace: Workspace]
  openChannelSettings: []
}>()

const { t } = useI18n()
const toast = useToast()

const enabled = ref(true)
const waitingHuman = ref(true)
const failed = ref(true)
const completed = ref(false)
const waitingHumanTemplate = ref('')
const failedTemplate = ref('')
const completedTemplate = ref('')
const templateKind = ref<RunNotifyKind>('waiting_human')
const hasChannel = ref(false)
const loadingChannel = ref(true)
const channelError = ref<string | null>(null)
const saving = ref(false)
const tplInput = ref<HTMLTextAreaElement | null>(null)

function policyFromWorkspace(p: Workspace): WorkspaceNotifyPolicy {
  const raw = p.notifyPolicy
  return {
    enabled: raw?.enabled !== false,
    defaultEvents: Array.isArray(raw?.defaultEvents)
      ? [...raw!.defaultEvents!]
      : ['waiting_human', 'failed'],
    waitingHumanTemplate: raw?.waitingHumanTemplate ?? '',
    failedTemplate: raw?.failedTemplate ?? '',
    completedTemplate: raw?.completedTemplate ?? '',
  }
}

function syncFromWorkspace() {
  const p = policyFromWorkspace(props.workspace)
  enabled.value = p.enabled !== false
  const ev = new Set(p.defaultEvents || [])
  waitingHuman.value = ev.has('waiting_human')
  failed.value = ev.has('failed')
  completed.value = ev.has('completed')
  waitingHumanTemplate.value = p.waitingHumanTemplate || ''
  failedTemplate.value = p.failedTemplate || ''
  completedTemplate.value = p.completedTemplate || ''
}

async function loadChannel() {
  loadingChannel.value = true
  channelError.value = null
  try {
    const ids = Array.isArray(props.workspace.notifyPolicy?.channelIds)
      ? props.workspace.notifyPolicy!.channelIds!.filter(Boolean)
      : []
    if (!ids.length) {
      hasChannel.value = false
      return
    }
    const res = await api.listChannels()
    const byId = new Map((res.items || []).map((c) => [c.id, c]))
    hasChannel.value = ids.some((id) => {
      const ch = byId.get(id)
      return !!(ch && ch.enabled && String(ch.cronDeliverTarget || '').trim())
    })
  } catch (err) {
    hasChannel.value = false
    channelError.value =
      err instanceof Error && err.message
        ? err.message
        : String(t('pages.workspace.notify.loadChannelFailed'))
  } finally {
    loadingChannel.value = false
  }
}

watch(
  () => props.workspace,
  () => syncFromWorkspace(),
  { immediate: true },
)

onMounted(() => {
  void loadChannel()
})

const statusLabel = computed(() => {
  if (loadingChannel.value) return t('common.loading.inProgress')
  if (channelError.value) return t('common.loading.failed')
  if (!enabled.value) return t('pages.workspace.notify.statusOff')
  if (!hasChannel.value) return t('pages.workspace.notify.statusNoChannel')
  return t('pages.workspace.notify.statusOn')
})

const controlsDisabled = computed(() => loadingChannel.value || !!channelError.value || saving.value)

const currentTemplate = computed({
  get() {
    if (templateKind.value === 'waiting_human') return waitingHumanTemplate.value
    if (templateKind.value === 'completed') return completedTemplate.value
    return failedTemplate.value
  },
  set(v: string) {
    if (templateKind.value === 'waiting_human') waitingHumanTemplate.value = v
    else if (templateKind.value === 'completed') completedTemplate.value = v
    else failedTemplate.value = v
  },
})

const previewText = computed(() =>
  renderRunNotifyMessage(templateKind.value, currentTemplate.value),
)

const previewModeLabel = computed(() => {
  const empty = !String(currentTemplate.value || '').trim()
  return empty
    ? t('pages.workspace.notify.previewDefault')
    : t('pages.workspace.notify.previewCustom')
})

const previewIsDefault = computed(() => !String(currentTemplate.value || '').trim())

async function save() {
  saving.value = true
  try {
    const defaultEvents: string[] = []
    if (waitingHuman.value) defaultEvents.push('waiting_human')
    if (failed.value) defaultEvents.push('failed')
    if (completed.value) defaultEvents.push('completed')
    // Always round-trip templates so enabled/events-only saves cannot wipe them.
    const notifyPolicy: WorkspaceNotifyPolicy = {
      enabled: enabled.value,
      defaultEvents,
      waitingHumanTemplate: waitingHumanTemplate.value,
      failedTemplate: failedTemplate.value,
      completedTemplate: completedTemplate.value,
    }
    const updated = await api.updateWorkspace({ notifyPolicy })
    emit('updated', updated)
    toast.success(t('pages.workspace.notify.saved'))
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    saving.value = false
  }
}

function fillDefaultTemplate() {
  currentTemplate.value = defaultEditableRunNotifyTemplate(templateKind.value)
}

function clearTemplate() {
  currentTemplate.value = ''
}

async function insertPlaceholder(ph: string) {
  const el = tplInput.value
  if (!el) {
    currentTemplate.value = `${currentTemplate.value || ''}${ph}`
    return
  }
  const start = el.selectionStart ?? el.value.length
  const end = el.selectionEnd ?? start
  const v = el.value
  const next = v.slice(0, start) + ph + v.slice(end)
  currentTemplate.value = next
  await nextTick()
  el.focus()
  const pos = start + ph.length
  el.setSelectionRange(pos, pos)
}

function goChannelSettings() {
  emit('openChannelSettings')
}
</script>

<template>
  <div class="space-y-7" data-testid="workspace-notify-panel" :aria-busy="loadingChannel || saving">
    <div
      v-if="loadingChannel"
      class="space-y-3 py-4"
      data-testid="notify-channel-skeleton"
      aria-hidden="true"
    >
      <div class="h-4 w-1/3 animate-pulse bg-elevated" />
      <div class="h-3 w-2/3 animate-pulse bg-elevated" />
      <div class="h-10 w-full animate-pulse bg-elevated" />
      <div class="h-10 w-full animate-pulse bg-elevated" />
    </div>

    <div v-else-if="channelError" data-testid="notify-channel-error">
      <AppInlineError
        :title="t('pages.workspace.notify.loadChannelFailed')"
        :message="channelError"
        @retry="loadChannel"
      />
    </div>

    <template v-else>
      <SettingsSection
        :title="t('pages.workspace.notify.deliverySection')"
        :description="t('pages.workspace.notify.lead')"
      >
        <SettingsRow
          :label="t('pages.workspace.notify.master')"
          :description="t('pages.workspace.notify.masterHint')"
          inline
        >
          <template #meta>
            <span
              class="inline-flex items-center rounded-full border px-2 py-0.5 text-[11px] font-medium"
              :class="
                !enabled
                  ? 'border-err/40 bg-err/10 text-err'
                  : !hasChannel
                    ? 'border-warn/40 bg-warn/10 text-warn'
                    : 'border-ok/40 bg-ok/10 text-ok'
              "
              data-testid="notify-status-badge"
              role="status"
            >
              {{ statusLabel }}
            </span>
          </template>
          <AppSwitch
            v-model="enabled"
            data-testid="notify-master-toggle"
            :disabled="controlsDisabled"
            :aria-label="t('pages.workspace.notify.master')"
          />
          <template v-if="!hasChannel" #details>
            <p class="m-0 text-txt2" data-testid="notify-no-channel-hint">
              {{ t('pages.workspace.notify.noChannelHint') }}
              <button
                type="button"
                class="ml-1 text-accent-2 underline underline-offset-2 hover:brightness-110"
                data-testid="notify-go-channel"
                @click="goChannelSettings"
              >
                {{ t('pages.workspace.notify.goChannel') }}
              </button>
            </p>
          </template>
        </SettingsRow>
      </SettingsSection>

      <SettingsSection
        :title="t('pages.workspace.notify.defaultEvents')"
        :description="t('pages.workspace.notify.defaultEventsHint')"
      >
        <SettingsRow
          :label="t('pages.workspace.notify.evWaitingHumanLabel')"
          :description="t('pages.workspace.notify.evWaitingHuman')"
          inline
        >
          <AppSwitch
            v-model="waitingHuman"
            data-testid="notify-ev-waiting-human"
            :disabled="controlsDisabled"
            :aria-label="t('pages.workspace.notify.evWaitingHumanLabel')"
          />
        </SettingsRow>
        <SettingsRow
          :label="t('pages.workspace.notify.evFailedLabel')"
          :description="t('pages.workspace.notify.evFailed')"
          inline
        >
          <AppSwitch
            v-model="failed"
            data-testid="notify-ev-failed"
            :disabled="controlsDisabled"
            :aria-label="t('pages.workspace.notify.evFailedLabel')"
          />
        </SettingsRow>
        <SettingsRow
          :label="t('pages.workspace.notify.evCompletedLabel')"
          :description="t('pages.workspace.notify.evCompleted')"
          inline
        >
          <AppSwitch
            v-model="completed"
            data-testid="notify-ev-completed"
            :disabled="controlsDisabled"
            :aria-label="t('pages.workspace.notify.evCompletedLabel')"
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection
        :title="t('pages.workspace.notify.templatesTitle')"
        :description="t('pages.workspace.notify.templatesHint')"
        data-testid="notify-template-section"
      >
        <SettingsRow :label="t('pages.workspace.notify.templateEvent')">
          <SlidingTabs
            :value="templateKind"
            class="flex gap-1 rounded-lg bg-elevated p-1"
            role="tablist"
            :aria-label="t('pages.workspace.notify.templateEvent')"
            data-testid="notify-template-seg"
          >
            <button
              type="button"
              role="tab"
              class="flex-1 whitespace-nowrap rounded-md px-3 py-1.5 text-[12px] transition disabled:opacity-55"
              :class="templateKind === 'waiting_human' ? 'text-txt' : 'text-txt3 hover:text-txt2'"
              :aria-selected="templateKind === 'waiting_human'"
              :disabled="controlsDisabled"
              data-testid="notify-tpl-seg-waiting"
              @click="templateKind = 'waiting_human'"
            >
              {{ t('pages.workspace.notify.segWaitingHuman') }}
            </button>
            <button
              type="button"
              role="tab"
              class="flex-1 whitespace-nowrap rounded-md px-3 py-1.5 text-[12px] transition disabled:opacity-55"
              :class="templateKind === 'failed' ? 'text-txt' : 'text-txt3 hover:text-txt2'"
              :aria-selected="templateKind === 'failed'"
              :disabled="controlsDisabled"
              data-testid="notify-tpl-seg-failed"
              @click="templateKind = 'failed'"
            >
              {{ t('pages.workspace.notify.segFailed') }}
            </button>
            <button
              type="button"
              role="tab"
              class="flex-1 whitespace-nowrap rounded-md px-3 py-1.5 text-[12px] transition disabled:opacity-55"
              :class="templateKind === 'completed' ? 'text-txt' : 'text-txt3 hover:text-txt2'"
              :aria-selected="templateKind === 'completed'"
              :disabled="controlsDisabled"
              data-testid="notify-tpl-seg-completed"
              @click="templateKind = 'completed'"
            >
              {{ t('pages.workspace.notify.segCompleted') }}
            </button>
          </SlidingTabs>
        </SettingsRow>

        <SettingsRow
          :label="t('pages.workspace.notify.templateBody')"
          :description="t('pages.workspace.notify.templatePlaceholder')"
          for="notify-template-input"
          stacked
        >
          <div class="space-y-3">
            <div class="flex flex-wrap items-center gap-1.5" data-testid="notify-placeholder-chips">
              <span class="mr-1 text-[11px] text-txt3">{{ t('pages.workspace.notify.placeholders') }}</span>
              <button
                v-for="ph in RUN_NOTIFY_PLACEHOLDERS"
                :key="ph"
                type="button"
                class="rounded border border-line bg-elevated px-1.5 py-0.5 font-mono text-[11px] text-accent-2 transition hover:border-accent/50 disabled:opacity-55"
                :data-testid="`notify-ph-${ph.replace(/[{}]/g, '')}`"
                :disabled="controlsDisabled"
                @click="insertPlaceholder(ph)"
              >
                {{ ph }}
              </button>
            </div>
            <textarea
              id="notify-template-input"
              ref="tplInput"
              v-model="currentTemplate"
              class="min-h-[160px] w-full resize-y rounded-lg border border-line bg-elevated px-3 py-2 font-mono text-[12.5px] leading-relaxed text-txt placeholder:text-txt3 focus:border-accent/55 focus:outline-none disabled:opacity-55"
              data-testid="notify-tpl-input"
              :placeholder="t('pages.workspace.notify.templatePlaceholder')"
              :disabled="controlsDisabled"
              spellcheck="false"
            />
            <div class="flex flex-wrap gap-2">
              <AppButton
                variant="outline"
                data-testid="notify-tpl-fill-default"
                :disabled="controlsDisabled"
                @click="fillDefaultTemplate"
              >
                {{ t('pages.workspace.notify.fillDefault') }}
              </AppButton>
              <AppButton
                variant="outline"
                data-testid="notify-tpl-clear"
                :disabled="controlsDisabled"
                @click="clearTemplate"
              >
                {{ t('pages.workspace.notify.clearDefault') }}
              </AppButton>
            </div>
          </div>
          <template #details>
            <div data-testid="notify-preview">
              <div class="mb-2 flex items-center justify-between gap-2">
                <span class="font-medium text-txt2">
                  {{ t('pages.workspace.notify.previewTitle') }}
                </span>
                <span
                  class="rounded-full px-2 py-0.5 text-[11px]"
                  :class="previewIsDefault ? 'bg-accent-dim/40 text-accent-2' : 'bg-ok/10 text-ok'"
                  data-testid="notify-preview-mode"
                >
                  {{ previewModeLabel }}
                </span>
              </div>
              <pre
                class="m-0 whitespace-pre-wrap break-all py-2 font-mono text-[12.5px] leading-relaxed text-txt2"
                data-testid="notify-preview-body"
              >{{ previewText }}</pre>
            </div>
          </template>
        </SettingsRow>
      </SettingsSection>

      <div class="flex justify-end">
        <AppButton
          variant="primary"
          :disabled="controlsDisabled"
          data-testid="notify-save"
          @click="save"
        >
          {{ saving ? t('common.buttons.saving') : t('common.buttons.save') }}
        </AppButton>
      </div>
    </template>
  </div>
</template>
