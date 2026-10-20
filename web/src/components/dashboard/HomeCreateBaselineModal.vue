<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import ReposEditor, { type RepoRow } from '@/components/ui/ReposEditor.vue'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{
  (e: 'close'): void
  (e: 'created', workflow: { id: string; name: string }): void
}>()
const toast = useToast()
const { t } = useI18n()
const { ensureLoaded } = useWorkspaceContext()
const loading = ref(false)
const loadError = ref('')
const baselineName = ref('')
const baselineRepos = ref<RepoRow[]>([{ name: '', url: '', branch: '' }])
const creating = ref(false)
const createError = ref('')
const nameInput = ref<HTMLInputElement | null>(null)
const canSubmit = computed(() => !!baselineName.value.trim() && baselineRepos.value.some((repo) => !!repo.url.trim()))
let generation = 0
async function load() {
  const gen = ++generation
  loading.value = true
  loadError.value = ''
  try {
    const workspace = await ensureLoaded()
    if (gen !== generation) return
    if (!workspace) throw new Error(t('pages.dashboard.create.loadFailed'))
    loading.value = false
    await nextTick()
    nameInput.value?.focus()
  } catch (e: any) {
    if (gen === generation) loadError.value = String(e?.message || e)
  } finally {
    if (gen === generation) loading.value = false
  }
}
function close() { if (!creating.value) emit('close') }
async function submit() {
  if (!canSubmit.value || creating.value) return
  creating.value = true
  createError.value = ''
  try {
    const created = await api.createWorkflowFromBaseline(baselineName.value.trim(), baselineRepos.value)
    toast.success(t('pages.workspace.newWorkflow.created', { name: created.name }))
    emit('created', { id: created.id, name: created.name })
    emit('close')
  } catch (e: any) {
    createError.value = String(e?.message || e)
  } finally { creating.value = false }
}
watch(() => props.open, (open) => {
  if (!open) { generation++; return }
  baselineName.value = ''
  baselineRepos.value = [{ name: '', url: '', branch: '' }]
  createError.value = ''
  void load()
}, { immediate: true })
</script>

<template>
  <AppModal :open="open" :title="t('pages.workspace.newWorkflow.modalTitle')" :width="460" close-on-esc :close-on-backdrop="!creating" @close="close">
    <div v-if="loading" class="py-6 text-center text-sm text-txt3" data-testid="home-create-loading">{{ t('common.loading.inProgress') }}</div>
    <div v-else-if="loadError" data-testid="home-create-load-error" role="alert">
      <p class="text-[13px] text-err">{{ loadError }}</p>
      <AppButton data-testid="home-create-retry" @click="load">{{ t('common.buttons.retry') }}</AppButton>
    </div>
    <div v-else data-testid="home-create-form">
      <p class="mb-3 text-[13px] text-txt2">{{ t('pages.workspace.newWorkflow.modalHint') }}</p>
      <div class="mb-4">
        <label class="label" for="home-create-workflow-name">{{ t('pages.workspace.newWorkflow.nameLabel') }}</label>
        <input id="home-create-workflow-name" ref="nameInput" v-model="baselineName" class="input" autocomplete="off" data-testid="home-create-workflow-name" :placeholder="t('pages.workspace.newWorkflow.namePlaceholder')" :disabled="creating" />
      </div>
      <ReposEditor :repos="baselineRepos" :min-rows="1" :editable="!creating" @update:repos="baselineRepos = $event" />
      <div v-if="createError" class="mt-3 flex gap-2 text-[12px] text-err" role="alert" data-testid="home-create-error"><Icon name="alert" :size="14" />{{ createError }}</div>
    </div>
    <template #footer>
      <AppButton variant="ghost" :disabled="creating" @click="close">{{ t('common.buttons.cancel') }}</AppButton>
      <AppButton v-if="!loading && !loadError" variant="primary" :loading="creating" :disabled="!canSubmit" data-testid="home-create-submit" @click="submit">{{ creating ? t('pages.workspace.newWorkflow.creating') : t('pages.workspace.newWorkflow.create') }}</AppButton>
    </template>
  </AppModal>
</template>
