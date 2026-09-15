<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { copyToClipboard } from '@/lib/shared/copyToClipboard'
import type { GateShareInboxStatus, ShareLinkTarget } from '@/lib/shared/types'
import {
  DEFAULT_GATE_SHARE_PERMISSION,
  DEFAULT_GATE_SHARE_TTL,
  GATE_SHARE_PERMISSION_PRESETS,
  GATE_SHARE_TTL_TIERS,
  canCreateGateShare,
  forgetShareUrl,
  isGateShareActive,
  isLoopbackShareHost,
  maskShareUrl,
  normalizePermissionPreset,
  recallShareUrl,
  rememberShareUrl,
  shareApiErrorMessage,
  type GateSharePermissionPreset,
  type GateShareTTLTier,
} from '@/lib/inbox/gateShareLink'

const props = defineProps<{
  open: boolean
  target: ShareLinkTarget | null
  kind?: 'human_gate' | 'review' | string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'updated', status: GateShareInboxStatus, url?: string): void
  (e: 'revoked', status: GateShareInboxStatus): void
}>()

const { t } = useI18n()
const toast = useToast()

const ttlTier = ref<GateShareTTLTier>(DEFAULT_GATE_SHARE_TTL)
const permissionPreset = ref<GateSharePermissionPreset>(DEFAULT_GATE_SHARE_PERMISSION)
const fullUrl = ref('')
const revealUrl = ref(false)
const busy = ref(false)
const busyKind = ref<'create' | 'copy' | null>(null)
const confirmKind = ref<'regen' | 'revoke' | null>(null)
const localStatus = ref<GateShareInboxStatus | null>(null)
const errorText = ref('')

const shareKind = computed(() => (props.kind === 'review' || props.target?.kind === 'review' ? 'review' : 'human_gate'))
const status = computed(() => localStatus.value || props.target?.shareLink || { state: 'none' })
const mode = computed(() => {
  if (status.value.state === 'used' || (!canCreateGateShare(status.value) && !isGateShareActive(status.value))) {
    return 'readonly' as const
  }
  if (isGateShareActive(status.value)) return 'manage' as const
  return 'create' as const
})
const displayUrl = computed(() => {
  if (!fullUrl.value) return ''
  return revealUrl.value ? fullUrl.value : maskShareUrl(fullUrl.value)
})
const activePreset = computed(() => normalizePermissionPreset(status.value.permissionPreset))
const activePresetChip = computed(() =>
  activePreset.value === 'react_only'
    ? t('pages.gatesInbox.share.permission.reactOnlyChip')
    : t('pages.gatesInbox.share.permission.fullChip'),
)

/** Prefer minted URL hostname; fall back to current admin entry for create-mode hints. */
const loopbackBlocked = computed(() => {
  if (fullUrl.value) return isLoopbackShareHost(fullUrl.value)
  if (typeof window !== 'undefined' && window.location?.hostname) {
    return isLoopbackShareHost(window.location.hostname)
  }
  return false
})
const copyAllowed = computed(() => !!fullUrl.value && !loopbackBlocked.value)

watch(
  () => [props.open, props.target?.runId, props.target?.nodeId, shareKind.value] as const,
  () => {
    if (!props.open) {
      revealUrl.value = false
      fullUrl.value = ''
      confirmKind.value = null
      errorText.value = ''
      return
    }
    localStatus.value = props.target?.shareLink || { state: 'none' }
    ttlTier.value =
      (props.target?.shareLink?.ttlTier as GateShareTTLTier) || DEFAULT_GATE_SHARE_TTL
    permissionPreset.value = normalizePermissionPreset(props.target?.shareLink?.permissionPreset)
    revealUrl.value = false
    fullUrl.value = recallShareUrl(props.target?.runId || '', props.target?.nodeId || '', props.target?.iteration)
    confirmKind.value = null
    errorText.value = ''
  },
  { immediate: true },
)

/** Dismiss same-message clipboardFallback toasts so rapid retries do not stack (plan g3.2). */
function showClipboardFallbackToast() {
  const msg = t('pages.gatesInbox.share.clipboardFallback')
  for (const item of [...toast.toasts.value]) {
    if (item.message === msg) toast.dismiss(item.id)
  }
  toast.show(msg)
}

/**
 * Write share URL via shared helper (secure API + non-secure execCommand fallback).
 * @param opts.auto — create/regen auto-copy uses autoCopied; manual copy uses copied (plan g1/g2).
 */
async function copyText(text: string, opts?: { auto?: boolean }) {
  if (isLoopbackShareHost(text)) return false
  const ok = await copyToClipboard(text)
  if (ok) {
    toast.success(
      t(opts?.auto ? 'pages.gatesInbox.share.autoCopied' : 'pages.gatesInbox.share.copied'),
    )
    revealUrl.value = false
    return true
  }
  revealUrl.value = true
  showClipboardFallbackToast()
  return false
}

async function createAndCopy() {
  if (!props.target || busy.value) return
  busy.value = true
  busyKind.value = 'create'
  errorText.value = ''
  try {
    const res =
      shareKind.value === 'review'
        ? await api.createReviewShareLink(
            props.target.runId,
            props.target.nodeId,
            ttlTier.value,
            permissionPreset.value,
          )
        : await api.createGateShareLink(
            props.target.runId,
            props.target.nodeId,
            ttlTier.value,
            permissionPreset.value,
          )
    fullUrl.value = res.url
    rememberShareUrl(props.target.runId, props.target.nodeId, props.target.iteration, res.url)
    const next: GateShareInboxStatus = {
      state: res.state || 'active',
      ttlTier: res.ttlTier,
      permissionPreset: normalizePermissionPreset(res.permissionPreset || permissionPreset.value),
      expiresAt: res.expiresAt,
      canCreate: false,
      canManage: true,
      remainingSec: undefined,
    }
    localStatus.value = next
    emit('updated', next, res.url)
    if (!isLoopbackShareHost(res.url)) {
      await copyText(res.url, { auto: true })
    }
  } catch (e) {
    errorText.value = shareApiErrorMessage(e, t)
  } finally {
    busy.value = false
    busyKind.value = null
  }
}

async function copyExisting() {
  if (!fullUrl.value) {
    errorText.value = t('pages.gatesInbox.share.copyUnavailable')
    return
  }
  if (isLoopbackShareHost(fullUrl.value)) {
    errorText.value = t('pages.gatesInbox.share.loopbackCopyBlocked')
    return
  }
  if (busy.value) return
  busy.value = true
  busyKind.value = 'copy'
  try {
    await copyText(fullUrl.value)
  } finally {
    busy.value = false
    busyKind.value = null
  }
}

async function confirmRegen() {
  if (!props.target || busy.value) return
  busy.value = true
  errorText.value = ''
  try {
    const res =
      shareKind.value === 'review'
        ? await api.regenReviewShareLink(props.target.runId, props.target.nodeId)
        : await api.regenGateShareLink(props.target.runId, props.target.nodeId)
    fullUrl.value = res.url
    rememberShareUrl(props.target.runId, props.target.nodeId, props.target.iteration, res.url)
    revealUrl.value = false
    confirmKind.value = null
    const next: GateShareInboxStatus = {
      state: res.state || 'active',
      ttlTier: res.ttlTier,
      permissionPreset: normalizePermissionPreset(res.permissionPreset || activePreset.value),
      expiresAt: res.expiresAt,
      canCreate: false,
      canManage: true,
    }
    localStatus.value = next
    emit('updated', next, res.url)
    // plan g2.3: regenerate feedback first, then auto-copy result (success or fallback)
    toast.success(t('pages.gatesInbox.share.regenerated'))
    if (!isLoopbackShareHost(res.url)) {
      await copyText(res.url, { auto: true })
    }
  } catch (e) {
    errorText.value = shareApiErrorMessage(e, t)
  } finally {
    busy.value = false
  }
}

async function confirmRevoke() {
  if (!props.target || busy.value) return
  busy.value = true
  errorText.value = ''
  try {
    if (shareKind.value === 'review') {
      await api.revokeReviewShareLink(props.target.runId, props.target.nodeId)
    } else {
      await api.revokeGateShareLink(props.target.runId, props.target.nodeId)
    }
    forgetShareUrl(props.target.runId, props.target.nodeId, props.target.iteration)
    const next: GateShareInboxStatus = {
      state: 'revoked',
      canCreate: true,
      canManage: false,
      ttlTier: status.value.ttlTier,
    }
    localStatus.value = next
    confirmKind.value = null
    emit('revoked', next)
    emit('close')
  } catch (e) {
    errorText.value = shareApiErrorMessage(e, t)
  } finally {
    busy.value = false
  }
}

function close() {
  emit('close')
}
</script>

<template>
  <AppModal
    :open="open"
    :title="t('pages.gatesInbox.share.panelTitle')"
    :width="520"
    close-on-esc
    @close="close"
  >
    <div v-if="target" class="space-y-4 text-sm text-txt" data-testid="gate-share-panel-body">
      <p class="rounded-lg border border-warn/35 bg-warn/10 px-3 py-2 text-[13px] text-warn" role="note">
        {{ t('pages.gatesInbox.share.safetyHint') }}
      </p>

      <p
        v-if="loopbackBlocked"
        class="rounded-lg border border-err/40 bg-err/10 px-3 py-2 text-[13px] text-err"
        role="alert"
        data-testid="gate-share-loopback-warning"
      >
        {{ t('pages.gatesInbox.share.loopbackWarning') }}
      </p>
      <p
        v-else
        class="rounded-lg border border-ok/35 bg-ok/10 px-3 py-2 text-[13px] text-ok"
        role="note"
        data-testid="gate-share-origin-hint"
      >
        {{ t('pages.gatesInbox.share.originFromAccessHint') }}
      </p>

      <div v-if="mode === 'readonly'" class="space-y-2" role="status">
        <p class="text-txt2">{{ t('pages.gatesInbox.share.usedReadonly') }}</p>
      </div>

      <div v-else-if="mode === 'create'" class="space-y-3">
        <label class="block text-xs font-medium text-txt2" for="gate-share-ttl">
          {{ t('pages.gatesInbox.share.ttlLabel') }}
        </label>
        <div id="gate-share-ttl" class="flex flex-wrap gap-2" role="radiogroup" :aria-label="t('pages.gatesInbox.share.ttlLabel')">
          <button
            v-for="tier in GATE_SHARE_TTL_TIERS"
            :key="tier"
            type="button"
            class="rounded-md min-h-11 border px-3 text-xs"
            :class="ttlTier === tier ? 'border-accent bg-accent text-white' : 'border-line text-txt2 hover:bg-elevated'"
            :aria-checked="ttlTier === tier ? 'true' : 'false'"
            role="radio"
            data-testid="gate-share-ttl"
            :data-tier="tier"
            @click="ttlTier = tier"
          >
            {{ t(`pages.gatesInbox.share.ttl.${tier}`) }}
          </button>
        </div>

        <label class="block text-xs font-medium text-txt2" id="gate-share-permission-label">
          {{ t('pages.gatesInbox.share.permissionLabel') }}
        </label>
        <div
          class="flex flex-col gap-2"
          role="radiogroup"
          aria-labelledby="gate-share-permission-label"
          data-testid="gate-share-permission-group"
        >
          <button
            v-for="preset in GATE_SHARE_PERMISSION_PRESETS"
            :key="preset"
            type="button"
            class="rounded-lg flex min-h-11 items-start gap-3 border px-3 py-2.5 text-left"
            :class="
              permissionPreset === preset
                ? 'border-accent bg-accent/10'
                : 'border-line bg-base text-txt2 hover:border-line-strong hover:bg-elevated'
            "
            :aria-checked="permissionPreset === preset ? 'true' : 'false'"
            role="radio"
            data-testid="gate-share-permission"
            :data-preset="preset"
            @click="permissionPreset = preset"
          >
            <span
              class="rounded-lg mt-0.5 grid h-3.5 w-3.5 shrink-0 place-items-center border"
              :class="permissionPreset === preset ? 'border-accent' : 'border-line-strong'"
              aria-hidden="true"
            >
              <span
                v-if="permissionPreset === preset"
                class="h-2 w-2 bg-accent"
              />
            </span>
            <span class="min-w-0">
              <span class="block text-[13px] font-semibold text-txt">
                {{ t(`pages.gatesInbox.share.permission.${preset}`) }}
              </span>
              <span class="mt-0.5 block text-xs text-txt3">
                {{ t(`pages.gatesInbox.share.permission.${preset}Desc`) }}
              </span>
            </span>
          </button>
        </div>

        <button
          type="button"
          class="rounded-md inline-flex min-h-11 w-full items-center justify-center gap-2 bg-accent px-3 text-sm font-medium text-white hover:bg-accent-hover disabled:opacity-45"
          data-testid="gate-share-create"
          :disabled="busy"
          :aria-busy="busy ? 'true' : undefined"
          :aria-label="loopbackBlocked ? t('pages.gatesInbox.share.createOnlyAria') : t('pages.gatesInbox.share.createCopyAria')"
          @click="createAndCopy"
        >
          <Icon :name="busy ? 'spinner' : 'copy'" :size="16" :class="busy ? 'animate-spin' : ''" aria-hidden="true" />
          {{
            busy
              ? t('common.buttons.creating')
              : loopbackBlocked
                ? t('pages.gatesInbox.share.createOnly')
                : t('pages.gatesInbox.share.createCopy')
          }}
        </button>
      </div>

      <div v-else class="space-y-3">
        <div class="flex flex-wrap items-center gap-2" data-testid="gate-share-active-meta">
          <span
            class="rounded-md border border-accent/45 bg-accent/10 px-2 py-0.5 text-[11px] text-accent-2"
            data-testid="gate-share-preset-chip"
          >
            {{ activePresetChip }}
          </span>
          <span class="text-xs text-txt3" data-testid="gate-share-regen-inherit-hint">
            {{ t('pages.gatesInbox.share.regenInheritsHint') }}
          </span>
        </div>
        <label class="block text-xs font-medium text-txt2">{{ t('pages.gatesInbox.share.linkLabel') }}</label>
        <textarea
          class="rounded-lg w-full border bg-elevated px-3 py-2 font-mono text-[12px] text-txt"
          :class="loopbackBlocked ? 'border-err/45' : 'border-line'"
          rows="3"
          readonly
          data-testid="gate-share-url"
          :value="displayUrl || t('pages.gatesInbox.share.urlHiddenUntilCopy')"
          @focus="($event.target as HTMLTextAreaElement).select()"
        />
        <p v-if="!fullUrl" class="text-xs text-txt3" role="status" data-testid="gate-share-copy-unavailable">
          {{ t('pages.gatesInbox.share.copyUnavailable') }}
        </p>
        <p
          v-else-if="loopbackBlocked"
          class="text-xs text-err"
          role="status"
          data-testid="gate-share-loopback-copy-hint"
        >
          {{ t('pages.gatesInbox.share.loopbackCopyBlocked') }}
        </p>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="rounded-md inline-flex min-h-11 items-center gap-1.5 bg-accent px-3 text-xs font-medium text-white hover:bg-accent-hover disabled:opacity-45"
            data-testid="gate-share-copy"
            :disabled="busy || !copyAllowed"
            :aria-busy="busy ? 'true' : undefined"
            @click="copyExisting"
          >
            <Icon :name="busy && busyKind === 'copy' ? 'spinner' : 'copy'" :size="14" :class="busy && busyKind === 'copy' ? 'animate-spin' : ''" aria-hidden="true" />
            {{ busy && busyKind === 'copy' ? t('common.buttons.copying') : t('common.buttons.copy') }}
          </button>
          <button
            type="button"
            class="rounded-md inline-flex min-h-11 items-center border border-line px-3 text-xs text-txt2 hover:bg-elevated"
            data-testid="gate-share-regen"
            :disabled="busy"
            @click="confirmKind = 'regen'"
          >
            {{ t('pages.gatesInbox.share.regenerate') }}
          </button>
          <button
            type="button"
            class="rounded-md inline-flex min-h-11 items-center border border-err/40 px-3 text-xs text-err hover:bg-err/10"
            data-testid="gate-share-revoke"
            :disabled="busy"
            @click="confirmKind = 'revoke'"
          >
            {{ t('pages.gatesInbox.share.revoke') }}
          </button>
        </div>
      </div>

      <div
        v-if="confirmKind"
        class="rounded-lg border border-line bg-elevated px-3 py-3"
        data-testid="gate-share-confirm"
        role="alertdialog"
      >
        <p class="text-[13px] text-txt">
          {{
            confirmKind === 'regen'
              ? t('pages.gatesInbox.share.confirmRegen')
              : t('pages.gatesInbox.share.confirmRevoke')
          }}
        </p>
        <div class="mt-3 flex gap-2">
          <button
            type="button"
            class="rounded-md min-h-11 bg-accent px-3 text-xs font-medium text-white"
            data-testid="gate-share-confirm-ok"
            :disabled="busy"
            @click="confirmKind === 'regen' ? confirmRegen() : confirmRevoke()"
          >
            {{ t('common.buttons.confirm') }}
          </button>
          <button
            type="button"
            class="rounded-md min-h-11 border border-line px-3 text-xs text-txt2"
            data-testid="gate-share-confirm-cancel"
            @click="confirmKind = null"
          >
            {{ t('common.buttons.cancel') }}
          </button>
        </div>
      </div>

      <p v-if="errorText" class="text-xs text-err" role="alert" data-testid="gate-share-error">{{ errorText }}</p>
    </div>
  </AppModal>
</template>
