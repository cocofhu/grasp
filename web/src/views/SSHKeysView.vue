<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import SettingsPageHeader from '@/components/settings/SettingsPageHeader.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import { api } from '@/lib/api/api'
import type { UserSSHKey } from '@/lib/api/apiTypes'
import { useToast } from '@/lib/composables/useToast'
import { relTime } from '@/lib/shared/format'
import { useSSHGateway } from '@/lib/sandbox/useSSHGateway'

const { t } = useI18n()
const toast = useToast()
const gateway = useSSHGateway()

const keys = ref<UserSSHKey[]>([])
const loading = ref(true)
const loadError = ref('')
const name = ref('')
const publicKey = ref('')
const saving = ref(false)
const saveError = ref('')
const deleting = ref<UserSSHKey | null>(null)
const deleteBusy = ref(false)

const gatewayNotice = computed(() => {
  const info = gateway.info.value
  if (!info) return ''
  if (!info.enabled) return t('pages.sshKeys.gatewayOff')
  if (info.allowed === false) return t('pages.sshKeys.adminsOnly')
  return ''
})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    keys.value = (await api.listSSHKeys()).keys
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

async function add() {
  if (!publicKey.value.trim() || saving.value) return
  saving.value = true
  saveError.value = ''
  try {
    const key = await api.addSSHKey(publicKey.value.trim(), name.value.trim())
    keys.value = [...keys.value, key]
    name.value = ''
    publicKey.value = ''
    toast.show(t('pages.sshKeys.saved'))
    void gateway.load(true)
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}

async function confirmDelete() {
  const key = deleting.value
  if (!key) return
  deleteBusy.value = true
  try {
    await api.deleteSSHKey(key.id)
    keys.value = keys.value.filter((k) => k.id !== key.id)
    deleting.value = null
    toast.show(t('pages.sshKeys.deleted'))
    void gateway.load(true)
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e))
  } finally {
    deleteBusy.value = false
  }
}

onMounted(() => {
  void load()
  void gateway.load(true)
})
</script>

<template>
  <div class="ssh-keys-page" data-testid="ssh-keys-view">
    <SettingsPageHeader class="mb-6" icon="key" :title="t('pages.sshKeys.title')" :description="t('pages.sshKeys.subtitle')" />

    <p v-if="gatewayNotice" class="mb-4 flex items-start gap-2 rounded-lg border border-warn/30 bg-warn/10 px-3 py-2 text-[12.5px] text-warn" role="status" data-testid="ssh-keys-gateway-notice">
      <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />{{ gatewayNotice }}
    </p>

    <SettingsSection icon="plus" :title="t('pages.sshKeys.add')" :description="t('pages.sshKeys.howTo')">
      <form class="flex flex-col gap-3 py-4" data-testid="ssh-keys-form" @submit.prevent="add">
        <label class="flex flex-col gap-1 text-[12.5px] text-txt2">
          {{ t('pages.sshKeys.name') }}
          <input v-model="name" class="input w-full sm:w-[340px]" maxlength="100" :placeholder="t('pages.sshKeys.namePlaceholder')" data-testid="ssh-keys-name" />
        </label>
        <label class="flex flex-col gap-1 text-[12.5px] text-txt2">
          {{ t('pages.sshKeys.key') }}
          <textarea
            v-model="publicKey"
            class="input min-h-[88px] w-full resize-y font-mono text-[12px]"
            spellcheck="false"
            autocomplete="off"
            :placeholder="t('pages.sshKeys.keyPlaceholder')"
            data-testid="ssh-keys-public-key"
          />
        </label>
        <p v-if="saveError" class="m-0 text-[12px] text-err" role="alert" data-testid="ssh-keys-error">{{ saveError }}</p>
        <div>
          <AppButton type="submit" variant="primary" icon="plus" :loading="saving" :disabled="!publicKey.trim()" data-testid="ssh-keys-submit">{{ t('pages.sshKeys.add') }}</AppButton>
        </div>
      </form>
    </SettingsSection>

    <SettingsSection icon="key" :title="t('pages.sshKeys.title')" :count="loading ? undefined : keys.length">
      <div v-if="loading" class="py-8 text-center text-txt3"><Icon name="spinner" :size="16" class="inline" /></div>
      <div v-else-if="loadError" class="py-6 text-center text-[13px] text-err" role="alert">
        {{ loadError }}
        <AppButton class="ml-2" size="sm" variant="outline" @click="load">{{ t('common.buttons.retry') }}</AppButton>
      </div>
      <EmptyState v-else-if="!keys.length" icon="key" :title="t('pages.sshKeys.empty')" :desc="t('pages.sshKeys.emptyHint')" data-testid="ssh-keys-empty" />
      <ul v-else class="m-0 list-none p-0" data-testid="ssh-keys-list">
        <li v-for="key in keys" :key="key.id" class="ssh-key-row" data-testid="ssh-key-row">
          <Icon name="key" :size="16" class="mt-0.5 shrink-0 text-txt3" :interactive="false" />
          <div class="min-w-0 flex-1">
            <div class="truncate text-[13px] font-medium text-txt">{{ key.name }}</div>
            <div class="truncate font-mono text-[11.5px] text-txt3" :title="key.fingerprint">{{ key.fingerprint }}</div>
            <div class="mt-0.5 text-[11.5px] text-txt3">
              {{ t('pages.sshKeys.addedAt', { time: relTime(key.createdAt) }) }} ·
              {{ key.lastUsedAt ? t('pages.sshKeys.lastUsed', { time: relTime(key.lastUsedAt) }) : t('pages.sshKeys.neverUsed') }}
            </div>
          </div>
          <AppButton size="sm" variant="ghost" icon="trash" :aria-label="t('pages.sshKeys.delete')" data-testid="ssh-key-delete" @click="deleting = key">{{ t('pages.sshKeys.delete') }}</AppButton>
        </li>
      </ul>
    </SettingsSection>

    <AppModal :open="!!deleting" :title="t('pages.sshKeys.deleteTitle')" :width="420" close-on-esc @close="deleting = null">
      <p class="text-[13px] text-txt2" data-testid="ssh-key-delete-body">{{ t('pages.sshKeys.deleteBody', { name: deleting?.name || '' }) }}</p>
      <template #footer>
        <AppButton variant="ghost" @click="deleting = null">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="danger" icon="trash" :loading="deleteBusy" data-testid="ssh-key-delete-confirm" @click="confirmDelete">{{ t('pages.sshKeys.delete') }}</AppButton>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
.ssh-keys-page { width: 100%; max-width: 960px; margin-inline: auto; padding-top: 36px; padding-bottom: 80px; }
.ssh-key-row { display: flex; align-items: flex-start; gap: 12px; padding: 14px 0; }
.ssh-key-row + .ssh-key-row { border-top: 1px solid rgb(var(--c-line) / .6); }
@media (max-width: 639px) {
  .ssh-keys-page { padding-top: 24px; padding-bottom: 56px; }
}
</style>
