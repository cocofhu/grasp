<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, type ModelBinding, type WorkspaceModelDefaults } from '@/lib/api/api'
import { ACP_BACKENDS } from '@/lib/shared/regionPolicy'
import ModelBindingEditor from '@/components/agent/ModelBindingEditor.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import CredentialProviderLogo from './CredentialProviderLogo.vue'
import { useRoutingCopy } from '@/lib/agent/modelRouting'
import { useToast } from '@/lib/composables/useToast'
const c = useRoutingCopy()
const toast = useToast()
const backend = ref('cursor')
const defaults = ref<WorkspaceModelDefaults>({ bindingsByBackend: {} })
const binding = ref<ModelBinding>()
const valid = ref(false)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const editorKey = computed(() => `${backend.value}:${loading.value}`)
const backendOptions = ACP_BACKENDS.map((tool) => ({ value: tool.id, label: tool.label }))
async function load() { loading.value = true; error.value = ''; try { defaults.value = await api.getModelDefaults(); select(backend.value, true) } catch (e) { error.value = String((e as Error).message || e) } finally { loading.value = false } }
function select(value: string, refresh = false) { if (!refresh && value === backend.value) return; backend.value = value; valid.value = false; binding.value = defaults.value.bindingsByBackend[value] ? JSON.parse(JSON.stringify(defaults.value.bindingsByBackend[value])) : undefined }
async function save() {
  if (!binding.value || binding.value.kind === 'inherit' || !valid.value) return
  saving.value = true
  try { const latest = await api.getModelDefaults(); defaults.value = await api.putModelDefaults({ bindingsByBackend: { ...latest.bindingsByBackend, [backend.value]: binding.value } }); toast.success(c.value.defaultSaved) } catch (e) { error.value = String((e as Error).message || e) } finally { saving.value = false }
}
onMounted(load)
</script>
<template><section class="mt-8 min-w-0" data-testid="workspace-model-defaults"><h3 class="m-0 text-sm font-semibold text-txt">{{ c.defaults }}</h3><p class="text-xs leading-5 text-txt3">{{ c.defaultsHint }}</p><p v-if="error" role="alert" class="text-xs text-err">{{ error }} <AppButton size="sm" @click="load">{{ c.retry }}</AppButton></p><template v-if="!loading"><div class="mb-3"><span class="label">{{ c.chooseTool }}</span><AppSelect :model-value="backend" :options="backendOptions" :disabled="saving" :aria-label="c.chooseTool" class="w-full sm:max-w-xs" searchable data-testid="workspace-model-default-backend" @update:model-value="select"><template #leading><CredentialProviderLogo :provider="backend" match="provider" /></template><template #option-leading="{ option }"><CredentialProviderLogo :provider="option.value" match="provider" /></template></AppSelect></div><ModelBindingEditor :key="editorKey" v-model="binding" :backend="backend" :allow-inherit="false" direct-only @validity="valid = $event" /><div class="mt-3 flex justify-end"><AppButton variant="primary" :disabled="!valid || saving || !binding || binding.kind === 'inherit'" :loading="saving" data-testid="workspace-model-default-save" @click="save">{{ c.saveDefault }}</AppButton></div></template></section></template>
