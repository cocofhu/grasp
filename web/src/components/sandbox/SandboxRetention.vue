<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type SandboxLease, type SandboxLeaseExtend, type SandboxView } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { formatRemaining } from './retention'

const props = withDefaults(defineProps<{ sandbox: SandboxView; compact?: boolean }>(), { compact: false })
const emit = defineEmits<{ (e: 'updated', value: SandboxView): void }>()

const { t } = useI18n()
const toast = useToast()
const now = ref(Date.now())
const extending = ref(false)
let tick: number | undefined

const leases = computed(() => props.sandbox.leases || [])
const retained = computed(() => props.sandbox.purpose === 'run' && leases.value.length > 0 && !props.sandbox.busy)
const remaining = computed(() => {
  if (!props.sandbox.destroyAt) return ''
  return formatRemaining(new Date(props.sandbox.destroyAt).getTime() - now.value, t('pages.sandboxes.ttl.soon'))
})

function leaseLabel(l: SandboxLease): string {
  const kind = t(`pages.sandboxes.retention.kinds.${l.kind}`, l.kind)
  if (!l.until) return t('pages.sandboxes.retention.untilRunEnd', { kind })
  return t('pages.sandboxes.retention.until', { kind, time: new Date(l.until).toLocaleTimeString() })
}

function leaseTitle(l: SandboxLease): string {
  return [l.reason, l.by && t('pages.sandboxes.retention.by', { by: l.by })].filter(Boolean).join(' · ')
}

async function extend(body: SandboxLeaseExtend) {
  if (extending.value) return
  extending.value = true
  try {
    const fresh = await api.extendSandboxLease(props.sandbox.id, body)
    emit('updated', fresh)
    toast.success(t('pages.sandboxes.retention.extended'))
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    extending.value = false
  }
}

onMounted(() => {
  tick = window.setInterval(() => (now.value = Date.now()), 1000)
})
onBeforeUnmount(() => {
  if (tick) clearInterval(tick)
})
</script>

<template>
  <div
    v-if="retained"
    class="flex flex-wrap items-center gap-1.5 text-[11px]"
    :class="compact ? '' : 'rounded-lg border border-line bg-base px-3 py-2'"
    data-testid="sandbox-retention"
  >
    <span class="text-txt3">{{ t('pages.sandboxes.retention.remaining') }}</span>
    <span class="font-mono text-txt" data-testid="sandbox-retention-remaining">{{ remaining || '—' }}</span>
    <span
      v-for="l in leases"
      :key="l.kind"
      class="chip border-line text-txt3"
      :title="leaseTitle(l)"
      data-testid="sandbox-retention-lease"
    >{{ leaseLabel(l) }}</span>
    <span class="ml-auto flex gap-1">
      <button
        type="button"
        class="rounded border border-line px-2 py-0.5 text-txt2 hover:border-line-strong disabled:opacity-40"
        :disabled="extending"
        data-testid="sandbox-retention-extend-30"
        @click="extend({ minutes: 30 })"
      >{{ t('pages.sandboxes.retention.extendMinutes', { n: 30 }) }}</button>
      <button
        type="button"
        class="rounded border border-line px-2 py-0.5 text-txt2 hover:border-line-strong disabled:opacity-40"
        :disabled="extending"
        data-testid="sandbox-retention-extend-120"
        @click="extend({ minutes: 120 })"
      >{{ t('pages.sandboxes.retention.extendHours', { n: 2 }) }}</button>
      <button
        v-if="sandbox.runId"
        type="button"
        class="rounded border border-line px-2 py-0.5 text-txt2 hover:border-line-strong disabled:opacity-40"
        :disabled="extending"
        data-testid="sandbox-retention-extend-run"
        @click="extend({ until: 'run_end' })"
      >{{ t('pages.sandboxes.retention.extendRunEnd') }}</button>
    </span>
  </div>
</template>
