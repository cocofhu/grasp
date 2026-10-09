<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import CredentialProviderLogo from '@/components/project/CredentialProviderLogo.vue'
import { api, type ProjectCredentialItem } from '@/lib/api/api'
import { kindById, kindOfItem, type CredentialKindId } from '@/lib/project/credentialKinds'
import { useToast } from '@/lib/composables/useToast'

const props = defineProps<{
  projectId: string
  kind: CredentialKindId
  selectedId?: string
  items?: ProjectCredentialItem[]
  title?: string
  hint?: string
}>()

const emit = defineEmits<{
  'update:selectedId': [id: string]
}>()

const { t } = useI18n()
const toast = useToast()
const remoteItems = ref<ProjectCredentialItem[]>([])

const kindDef = computed(() => kindById(props.kind))
const source = computed(() => (props.items === undefined ? remoteItems.value : props.items))
const rows = computed(() =>
  source.value.filter(
    (item) =>
      item.configured &&
      item.enabled !== false &&
      !item.revokedAt &&
      kindOfItem(item)?.id === props.kind,
  ),
)

function vendorOf(item: ProjectCredentialItem): string {
  const value = item.metadata?.provider
  return typeof value === 'string' ? value.trim() : ''
}

function modelOf(item: ProjectCredentialItem): string {
  const value = item.metadata?.model
  return typeof value === 'string' ? value.trim() : ''
}

function subtitle(item: ProjectCredentialItem): string {
  if (props.kind === 'opencode') {
    const vendor = vendorOf(item)
    const model = modelOf(item)
    if (vendor || model) {
      return t('pages.agentStudio.openCode.credentialVendorModel', { vendor, model })
    }
  }
  return t(`pages.projectDetail.projectCredentials.kinds.${props.kind}`)
}

async function loadRemote() {
  if (props.items !== undefined || !props.projectId) {
    remoteItems.value = []
    return
  }
  try {
    const response = await api.getProjectCredentials(props.projectId)
    remoteItems.value = response.items || []
  } catch (e: unknown) {
    remoteItems.value = []
    toast.error(String((e as { message?: string })?.message || e))
  }
}

function choose(id: string) {
  if (id === (props.selectedId || '')) return
  emit('update:selectedId', id)
}

function clear() {
  if (!props.selectedId) return
  emit('update:selectedId', '')
}

onMounted(() => {
  void loadRemote()
})

watch(
  () => props.projectId,
  () => {
    void loadRemote()
  },
)
</script>

<template>
  <div class="mt-3" data-testid="credential-alias-picker" :data-kind="kind">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="text-[12px] font-medium text-txt2">{{ title || t('pages.projectDetail.projectCredentials.pickTitle') }}</div>
        <p class="mb-0 mt-1 text-[11px] leading-5 text-txt3">{{ hint || t('pages.projectDetail.projectCredentials.pickHint') }}</p>
      </div>
      <AppButton
        v-if="selectedId"
        size="sm"
        variant="ghost"
        data-testid="credential-alias-clear"
        @click="clear"
      >
        {{ t('pages.projectDetail.projectCredentials.pickClear') }}
      </AppButton>
    </div>
    <p v-if="!projectId" class="mb-0 mt-2 text-[11px] text-txt3">{{ t('pages.agentStudio.openCode.credentialNeedProject') }}</p>
    <div
      v-else-if="!rows.length"
      class="mt-2 rounded-lg border border-dashed border-line px-3 py-3 text-[12px] text-txt3"
      data-testid="credential-alias-empty"
    >
      {{ t('pages.projectDetail.projectCredentials.pickEmpty') }}
    </div>
    <div v-else class="mt-2 space-y-2">
      <button
        v-for="item in rows"
        :key="item.id"
        type="button"
        class="flex w-full items-start gap-3 rounded-lg border px-3 py-2.5 text-left transition-[border-color,background-color] duration-[160ms] ease-[cubic-bezier(0.16,1,0.3,1)]"
        :class="selectedId === item.id ? 'border-accent bg-accent-dim' : 'border-line bg-base hover:border-line-strong'"
        :data-testid="`credential-alias-option-${item.id}`"
        :data-alias="item.name"
        :aria-pressed="selectedId === item.id"
        @click="choose(item.id)"
      >
        <CredentialProviderLogo
          :provider="kind === 'opencode' ? (vendorOf(item) || 'opencode') : kindDef?.logoProvider"
          :icon="kindDef?.icon"
          match="provider"
          :selected="selectedId === item.id"
          :configured="true"
        />
        <span class="min-w-0">
          <span class="block truncate text-[12px] font-semibold text-txt" data-testid="credential-alias-title">{{ item.name }}</span>
          <span class="mt-0.5 block text-[11px] text-txt3" data-testid="credential-alias-subtitle">{{ subtitle(item) }}</span>
        </span>
      </button>
    </div>
  </div>
</template>
