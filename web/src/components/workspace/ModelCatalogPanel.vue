<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type ModelConnection, type ModelConnectionBody } from '@/lib/api/api'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import Icon from '@/components/ui/Icon.vue'
import CredentialProviderLogo from './CredentialProviderLogo.vue'
import { useToast } from '@/lib/composables/useToast'

type CatalogModel = ModelConnection['models'][number]
type Capability = 'toolCalls' | 'vision' | 'reasoning'
const emit = defineEmits<{ changed: []; 'add-service': [] }>()
const { locale } = useI18n()
const toast = useToast()
const copy = computed(() => locale.value.startsWith('en') ? {
  title: 'Models', hint: 'Add models to a saved service, then select the service and model in an Agent.', add: 'Add model', edit: 'Edit model', remove: 'Delete', save: 'Save', cancel: 'Cancel', retry: 'Retry', loading: 'Loading…', empty: 'Configure a model service before adding models.', addService: 'Configure service', noModels: 'This service has no models yet.', service: 'Model service', selectService: 'Select a saved service', id: 'Original model ID', name: 'Display name (optional)', idHint: 'Use the exact ID accepted by this service, including any /.', details: 'Model capabilities (optional)', detailsHint: 'Use the provider’s specifications. These settings do not verify the endpoint.', input: 'Input types', unset: 'Not specified', text: 'Text', image: 'Image', both: 'Text and image', context: 'Context window', output: 'Maximum output tokens', efforts: 'Reasoning levels (comma separated)', toolCalls: 'Tool calling', vision: 'Image understanding', reasoning: 'Reasoning', yes: 'Supported', no: 'Not supported', disabled: 'Service disabled', count: 'models', saved: 'Model saved', deleted: 'Model deleted', required: 'Select a service and enter a model ID.', duplicate: 'This service already has that model ID.', number: 'Token limits must be positive whole numbers.', missing: 'The service or model changed. Refresh and try again.', deleteConfirm: 'Delete model “{model}” from “{service}”? Agents and tool defaults referencing it will become unavailable until explicitly updated. They will not switch to another model.', renameConfirm: 'Changing this model ID makes Agents and tool defaults using the old ID unavailable. Continue?',
} : {
  title: '模型', hint: '为已保存的服务添加模型，再到 Agent 中选择服务和模型。', add: '新增模型', edit: '编辑模型', remove: '删除', save: '保存', cancel: '取消', retry: '重试', loading: '加载中…', empty: '先配置模型服务，再为它添加模型。', addService: '配置服务', noModels: '此服务尚未添加模型。', service: '模型服务', selectService: '选择已保存的服务', id: '服务商原始模型 ID', name: '显示名称（可选）', idHint: '按服务商实际接受的 ID 填写，保留其中的 /。', details: '模型能力（可选）', detailsHint: '按供应商实际规格填写，这些配置不代表接口已验证。', input: '输入类型', unset: '未指定', text: '文本', image: '图像', both: '文本与图像', context: '上下文窗口', output: '最大输出 Token', efforts: '推理档位（逗号分隔）', toolCalls: '工具调用', vision: '图像理解', reasoning: '推理', yes: '支持', no: '不支持', disabled: '服务已停用', count: '个模型', saved: '模型已保存', deleted: '模型已删除', required: '请选择服务并填写模型 ID。', duplicate: '此服务已存在相同的模型 ID。', number: 'Token 上限必须是正整数。', missing: '服务或模型已变更，请刷新后重试。', deleteConfirm: '从“{service}”删除模型“{model}”？引用它的 Agent 和工具默认配置将不可用，需要明确重新配置，不会自动切换其他模型。', renameConfirm: '修改模型 ID 后，引用旧 ID 的 Agent 和工具默认配置将不可用。确认继续？',
})

const rows = ref<ModelConnection[]>([])
const loading = ref(true)
const loadError = ref('')
const formError = ref('')
const open = ref(false)
const saving = ref(false)
const originalId = ref('')
const selectedServiceId = ref('')
const capabilities: Capability[] = ['toolCalls', 'vision', 'reasoning']
const form = reactive({ id: '', name: '', input: '', contextWindow: '' as string | number, maxTokens: '' as string | number, efforts: '', toolCalls: '', vision: '', reasoning: '' })
const selectedService = computed(() => rows.value.find((row) => row.id === selectedServiceId.value))
const serviceOptions = computed(() => [{ value: '', label: copy.value.selectService }, ...rows.value.map((service) => ({ value: service.id, label: service.name, hint: service.providerId }))])
const inputOptions = computed(() => [{ value: '', label: copy.value.unset }, { value: 'text', label: copy.value.text }, { value: 'text,image', label: copy.value.both }, { value: 'image', label: copy.value.image }])
const capabilityOptions = computed(() => [{ value: '', label: copy.value.unset }, { value: 'yes', label: copy.value.yes }, { value: 'no', label: copy.value.no }])
const modelCount = computed(() => rows.value.reduce((count, row) => count + row.models.length, 0))
const validNumber = (value: string | number) => value === '' || /^\d+$/.test(String(value)) && Number.isSafeInteger(Number(value)) && Number(value) > 0
const valid = computed(() => !!selectedService.value && !!form.id.trim() && validNumber(form.contextWindow) && validNumber(form.maxTokens))

async function refresh() {
  loading.value = true
  loadError.value = ''
  try { rows.value = (await api.listModelConnections()).items || [] }
  catch (e) { loadError.value = String((e as Error).message || e) }
  finally { loading.value = false }
}

function start(service?: ModelConnection, model?: CatalogModel) {
  originalId.value = model?.id || ''
  selectedServiceId.value = service?.id || (rows.value.length === 1 ? rows.value[0]!.id : '')
  Object.assign(form, { id: model?.id || '', name: model?.name || '', input: model?.input?.join(',') || '', contextWindow: model?.contextWindow?.toString() || '', maxTokens: model?.maxTokens?.toString() || '', efforts: model?.reasoningEfforts?.join(', ') || '' })
  for (const field of capabilities) form[field] = model?.capabilities?.[field] === undefined ? '' : model.capabilities[field] ? 'yes' : 'no'
  formError.value = ''
  open.value = true
}

function buildModel(previous?: CatalogModel): CatalogModel {
  const next: CatalogModel = { ...previous, id: form.id.trim(), source: previous && previous.id === form.id.trim() ? previous.source : 'manual' }
  if (form.name.trim()) next.name = form.name.trim(); else delete next.name
  if (form.input) next.input = form.input.split(',') as ('text' | 'image')[]; else delete next.input
  if (form.contextWindow) next.contextWindow = Number(form.contextWindow); else delete next.contextWindow
  if (form.maxTokens) next.maxTokens = Number(form.maxTokens); else delete next.maxTokens
  const efforts = [...new Set(form.efforts.split(',').map((effort) => effort.trim()).filter(Boolean))]
  if (efforts.length) next.reasoningEfforts = efforts; else delete next.reasoningEfforts
  const flags = { ...previous?.capabilities }
  for (const field of capabilities) { if (form[field]) flags[field] = form[field] === 'yes'; else delete flags[field] }
  if (Object.keys(flags).length) next.capabilities = flags; else delete next.capabilities
  return next
}

// PUT replaces the connection on the server. Fetch it again before every model
// mutation so a service address, key reference or other models edited elsewhere
// are preserved. Never send a secret or write Agent/default bindings here.
async function updateModels(id: string, change: (models: CatalogModel[]) => CatalogModel[]) {
  const latest = (await api.listModelConnections()).items || []
  const current = latest.find((row) => row.id === id)
  if (!current) throw new Error(copy.value.missing)
  const body: ModelConnectionBody = { name: current.name, providerId: current.providerId, protocol: current.protocol, baseUrl: current.baseUrl, credentialId: current.credentialId, auth: { ...current.auth }, ...(current.service ? { service: { ...current.service } } : {}), state: current.state, models: change(current.models) }
  const saved = await api.updateModelConnection(id, body)
  rows.value = latest.map((row) => row.id === id ? saved : row)
  emit('changed')
}

async function save() {
  if (saving.value) return
  formError.value = ''
  if (!selectedService.value || !form.id.trim()) { formError.value = copy.value.required; return }
  if (!validNumber(form.contextWindow) || !validNumber(form.maxTokens)) { formError.value = copy.value.number; return }
  if (originalId.value && originalId.value !== form.id.trim() && !window.confirm(copy.value.renameConfirm)) return
  saving.value = true
  try {
    await updateModels(selectedServiceId.value, (models) => {
      if (models.some((model) => model.id === form.id.trim() && model.id !== originalId.value)) throw new Error(copy.value.duplicate)
      const previous = models.find((model) => model.id === originalId.value)
      if (originalId.value && !previous) throw new Error(copy.value.missing)
      const next = buildModel(previous)
      return previous ? models.map((model) => model.id === originalId.value ? next : model) : [...models, next]
    })
    open.value = false
    toast.success(copy.value.saved)
  } catch (e) { formError.value = String((e as Error).message || e) }
  finally { saving.value = false }
}

async function remove(service: ModelConnection, model: CatalogModel) {
  if (saving.value || !window.confirm(copy.value.deleteConfirm.replace('{model}', model.id).replace('{service}', service.name))) return
  saving.value = true
  loadError.value = ''
  try {
    await updateModels(service.id, (models) => models.filter((entry) => entry.id !== model.id))
    toast.success(copy.value.deleted)
  } catch (e) { loadError.value = String((e as Error).message || e) }
  finally { saving.value = false }
}

defineExpose({ refresh })
onMounted(refresh)
</script>

<template>
  <section class="min-w-0 space-y-5" data-testid="model-catalog-panel">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0"><h3 class="m-0 text-sm font-semibold text-txt">{{ copy.title }}<span v-if="rows.length" class="ml-2 text-xs font-normal text-txt3">{{ modelCount }}</span></h3><p class="mb-0 mt-1 text-xs leading-5 text-txt3">{{ copy.hint }}</p></div>
      <AppButton icon="plus" size="sm" variant="outline" :disabled="loading || !rows.length || saving" data-testid="model-catalog-add" @click="start()">{{ copy.add }}</AppButton>
    </header>
    <p v-if="loadError" role="alert" class="text-xs text-err">{{ loadError }} <AppButton size="sm" variant="ghost" data-testid="model-catalog-retry" @click="refresh">{{ copy.retry }}</AppButton></p>
    <p v-if="loading" role="status" class="text-xs text-txt3">{{ copy.loading }}</p>
    <div v-else-if="!rows.length && !loadError" class="flex flex-col items-center gap-3 rounded-xl border border-dashed border-line px-5 py-10 text-center" data-testid="model-catalog-empty">
      <Icon name="sparkles" :size="24" class="text-txt3" aria-hidden="true" /><p class="m-0 text-xs leading-5 text-txt3">{{ copy.empty }}</p><AppButton size="sm" variant="outline" data-testid="model-catalog-add-service" @click="emit('add-service')">{{ copy.addService }}</AppButton>
    </div>
    <section v-for="service in rows" v-else :key="service.id" class="min-w-0" :data-testid="`model-catalog-group-${service.id}`" :aria-labelledby="`model-catalog-heading-${service.id}`">
      <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
        <div class="flex min-w-0 items-center gap-3"><CredentialProviderLogo :provider="service.providerId" match="provider" /><div class="min-w-0"><h4 :id="`model-catalog-heading-${service.id}`" class="m-0 break-words text-[13px] font-semibold text-txt">{{ service.name }}</h4><p class="mb-0 mt-1 break-all text-[11px] text-txt3">{{ service.providerId }} · {{ service.protocol }} · {{ service.models.length }} {{ copy.count }}<span v-if="service.state === 'disabled'" class="ml-2 text-warn">{{ copy.disabled }}</span></p></div></div>
        <AppButton size="sm" variant="ghost" icon="plus" :disabled="saving" :data-testid="`model-catalog-add-${service.id}`" @click="start(service)">{{ copy.add }}</AppButton>
      </div>
      <div class="overflow-hidden rounded-xl border border-line bg-surface">
        <p v-if="!service.models.length" class="m-0 px-4 py-5 text-xs text-txt3">{{ copy.noModels }}</p>
        <article v-for="model in service.models" :key="model.id" class="model-catalog-row flex min-w-0 flex-wrap items-start justify-between gap-3 px-4 py-3" data-testid="model-catalog-row" :data-connection-id="service.id" :data-model-id="model.id">
          <div class="min-w-0 flex-1"><p class="m-0 break-words text-[13px] font-medium text-txt">{{ model.name || model.id }}</p><code class="mt-1 block text-[11px] text-txt3 [overflow-wrap:anywhere]">{{ model.id }}</code><p v-if="model.input?.length || model.contextWindow || model.maxTokens" class="mb-0 mt-1 text-[11px] leading-5 text-txt3">{{ model.input?.join(' + ') }}<span v-if="model.contextWindow"> · {{ copy.context }} {{ model.contextWindow.toLocaleString() }}</span><span v-if="model.maxTokens"> · {{ copy.output }} {{ model.maxTokens.toLocaleString() }}</span></p></div>
          <div class="flex shrink-0 gap-1"><AppButton size="sm" variant="outline" :disabled="saving" data-testid="model-catalog-edit" @click="start(service, model)">{{ copy.edit }}</AppButton><AppButton size="sm" variant="ghost" class="text-err" :disabled="saving" data-testid="model-catalog-delete" @click="remove(service, model)">{{ copy.remove }}</AppButton></div>
        </article>
      </div>
    </section>

    <AppModal :open="open" :title="originalId ? copy.edit : copy.add" :width="580" :close-on-esc="!saving" @close="!saving && (open = false)">
      <form id="model-catalog-form" class="space-y-4" data-testid="model-catalog-form" @submit.prevent="save">
        <label class="block"><span class="label">{{ copy.service }}</span><AppSelect v-model="selectedServiceId" :options="serviceOptions" :aria-label="copy.service" :disabled="!!originalId || saving" searchable data-testid="model-catalog-service"><template #leading><CredentialProviderLogo v-if="selectedService" :provider="selectedService.providerId" match="provider" /></template><template #option-leading="{ option }"><CredentialProviderLogo v-if="option.value" :provider="rows.find((row) => row.id === option.value)?.providerId" match="provider" /></template></AppSelect></label>
        <label class="block"><span class="label">{{ copy.id }}</span><input v-model="form.id" class="input w-full font-mono" :disabled="saving" data-testid="model-catalog-model-id" autocomplete="off" /><span class="mt-1 block text-[11px] leading-5 text-txt3">{{ copy.idHint }}</span></label>
        <label class="block"><span class="label">{{ copy.name }}</span><input v-model="form.name" class="input w-full" :disabled="saving" data-testid="model-catalog-model-name" autocomplete="off" /></label>
        <details class="rounded-lg border border-line p-3"><summary class="cursor-pointer text-xs font-medium text-txt2">{{ copy.details }}</summary><p class="mb-3 mt-2 text-[11px] leading-5 text-txt3">{{ copy.detailsHint }}</p><div class="space-y-3">
          <label class="block"><span class="label">{{ copy.input }}</span><AppSelect v-model="form.input" :options="inputOptions" :aria-label="copy.input" :disabled="saving" searchable data-testid="model-catalog-input" /></label>
          <div class="grid gap-3 sm:grid-cols-2"><label class="block min-w-0"><span class="label">{{ copy.context }}</span><input v-model="form.contextWindow" type="number" min="1" step="1" class="input w-full" data-testid="model-catalog-context-window" /></label><label class="block min-w-0"><span class="label">{{ copy.output }}</span><input v-model="form.maxTokens" type="number" min="1" step="1" class="input w-full" data-testid="model-catalog-max-tokens" /></label></div>
          <label v-for="field in capabilities" :key="field" class="block"><span class="label">{{ copy[field] }}</span><AppSelect v-model="form[field]" :options="capabilityOptions" :aria-label="copy[field]" :disabled="saving" searchable :data-testid="`model-catalog-capability-${field}`" /></label>
          <label class="block"><span class="label">{{ copy.efforts }}</span><input v-model="form.efforts" class="input w-full" data-testid="model-catalog-reasoning-efforts" autocomplete="off" /></label>
        </div></details>
        <p v-if="formError" role="alert" class="m-0 text-xs leading-5 text-err" data-testid="model-catalog-error">{{ formError }}</p>
      </form>
      <template #footer><AppButton variant="ghost" :disabled="saving" data-testid="model-catalog-cancel" @click="open = false">{{ copy.cancel }}</AppButton><AppButton variant="primary" type="submit" form="model-catalog-form" :disabled="!valid || saving" :loading="saving" data-testid="model-catalog-save">{{ copy.save }}</AppButton></template>
    </AppModal>
  </section>
</template>

<style scoped>
.model-catalog-row + .model-catalog-row { border-top: 1px solid rgb(var(--c-line) / .7); }
.model-catalog-row p { overflow-wrap: anywhere; }
@media (max-width: 479px) { .model-catalog-row > div:first-child { flex-basis: 100%; } }
</style>
