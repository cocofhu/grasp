<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import { api, type ProjectCredentialItem } from '@/lib/api/api'
import { fmtTime } from '@/lib/shared/format'
import { useToast } from '@/lib/composables/useToast'

const props = defineProps<{ projectId: string }>()

const { t } = useI18n()
const toast = useToast()

const loading = ref(true)
const loadError = ref('')
const items = ref<ProjectCredentialItem[]>([])
const drafts = reactive<Record<string, string>>({})
const saving = reactive<Record<string, boolean>>({})
const clearing = reactive<Record<string, boolean>>({})
const showCreate = ref(false)
const creating = ref(false)
const createType = ref('custom')
const createProvider = ref('')
const createName = ref('')
const createTarget = ref('')
const createEnvKey = ref('')
const createFallbackEnvKey = ref('')
const createValue = ref('')

const credentialTypes = ['ai', 'git', 'ssh', 'mcp', 'custom'] as const
const credentialTypeKeys = {
  ai: 'pages.projectDetail.projectCredentials.typeAi',
  git: 'pages.projectDetail.projectCredentials.typeGit',
  ssh: 'pages.projectDetail.projectCredentials.typeSsh',
  mcp: 'pages.projectDetail.projectCredentials.typeMcp',
  custom: 'pages.projectDetail.projectCredentials.typeCustom',
} as const
const createTargetMissing = computed(() =>
  createType.value === 'custom' && !createTarget.value.trim() && !createEnvKey.value.trim(),
)

type CredentialGroup = 'ai' | 'git' | 'ssh' | 'other'
const groupOrder: CredentialGroup[] = ['ai', 'git', 'ssh', 'other']

function groupFor(item: ProjectCredentialItem): CredentialGroup {
  const kind = (item.type || item.kind || '').toLowerCase()
  if (kind === 'ai') return 'ai'
  if (kind === 'git') return 'git'
  if (kind === 'ssh') return 'ssh'
  return 'other'
}

const orderedItems = computed(() =>
  [...items.value].sort((a, b) => {
    const aName = (a.name || a.provider || a.id).toLocaleLowerCase()
    const bName = (b.name || b.provider || b.id).toLocaleLowerCase()
    return aName.localeCompare(bName)
  }),
)

const configuredCount = computed(() => orderedItems.value.filter((item) => item.configured).length)
const apiKeyCount = computed(() => orderedItems.value.filter((item) => groupFor(item) === 'ai').length)
const credentialGroups = computed(() => groupOrder
  .map((id) => ({
    id,
    items: orderedItems.value.filter((item) => groupFor(item) === id),
  }))
  .filter((group) => group.items.length > 0))

function groupTitle(group: CredentialGroup): string {
  return t(`pages.projectDetail.projectCredentials.groups.${group}.title`)
}

function groupHint(group: CredentialGroup): string {
  return t(`pages.projectDetail.projectCredentials.groups.${group}.hint`)
}

function itemLabel(item: ProjectCredentialItem): string {
  return item.name || item.provider || item.envKey || item.id
}

function itemTarget(item: ProjectCredentialItem): string {
  if (item.target) return item.target
  if (item.envKey) return item.envKey
  if (item.provider) return item.provider
  return item.type || item.kind || ''
}

function isMultiline(item: ProjectCredentialItem): boolean {
  const key = `${item.type || item.kind || ''} ${item.provider || ''} ${item.envKey || ''}`.toLowerCase()
  return key.includes('ssh') || key.includes('private') || key.includes('known_hosts')
}

function configuredText(item: ProjectCredentialItem): string {
  return item.configured ? t('pages.projectDetail.projectCredentials.configured') : t('pages.projectDetail.projectCredentials.notConfigured')
}

function typeLabel(item: ProjectCredentialItem): string {
  const key = (item.type || item.kind || 'custom') as keyof typeof credentialTypeKeys
  return t(credentialTypeKeys[key] || credentialTypeKeys.custom)
}

function updateItems(next: ProjectCredentialItem) {
  const i = items.value.findIndex((item) => item.id === next.id)
  if (i < 0) items.value = [...items.value, next]
  else items.value = items.value.map((item, index) => (index === i ? next : item))
  drafts[next.id] = ''
}

function setItems(next: ProjectCredentialItem[]) {
  items.value = next
  for (const key of Object.keys(drafts)) delete drafts[key]
  for (const item of next) drafts[item.id] = ''
}

function openCreate() {
  createType.value = 'custom'
  createProvider.value = ''
  createName.value = ''
  createTarget.value = ''
  createEnvKey.value = ''
  createFallbackEnvKey.value = ''
  createValue.value = ''
  showCreate.value = true
}

async function create() {
  if (!createName.value.trim() || !createValue.value.trim() || creating.value) return
  creating.value = true
  try {
    const created = await api.createProjectCredential(props.projectId, {
      type: createType.value,
      provider: createProvider.value.trim(),
      name: createName.value.trim(),
      target: createTarget.value.trim(),
      envKey: createEnvKey.value.trim(),
      fallbackEnvKey: createFallbackEnvKey.value.trim(),
      value: createValue.value,
    })
    updateItems(created)
    showCreate.value = false
    toast.success(t('pages.projectDetail.projectCredentials.saved'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    creating.value = false
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const response = await api.getProjectCredentials(props.projectId)
    // Keep the client tolerant of an early backend response that returns the array directly.
    const next = Array.isArray(response) ? response : response?.items || []
    setItems(next)
  } catch (e: unknown) {
    loadError.value = String((e as { message?: string })?.message || e)
    setItems([])
  } finally {
    loading.value = false
  }
}

async function save(item: ProjectCredentialItem) {
  const value = drafts[item.id] || ''
  if (!value.trim() || saving[item.id]) return
  saving[item.id] = true
  try {
    const saved = await api.putProjectCredential(props.projectId, item.id, {
      type: item.type || item.kind,
      name: item.name,
      target: item.target,
      targetType: item.targetType,
      targetId: item.targetId,
      provider: item.provider,
      envKey: item.envKey,
      fallbackEnvKey: item.fallbackEnvKey,
      value,
      metadata: item.metadata,
      enabled: item.enabled,
    })
    updateItems(saved)
    toast.success(t('pages.projectDetail.projectCredentials.saved'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    saving[item.id] = false
  }
}

async function clear(item: ProjectCredentialItem) {
  if (clearing[item.id] || !item.configured) return
  clearing[item.id] = true
  try {
    await api.deleteProjectCredential(props.projectId, item.id)
    updateItems({ ...item, configured: false, masked: '', revokedAt: undefined })
    toast.success(t('pages.projectDetail.projectCredentials.cleared'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    clearing[item.id] = false
  }
}

function retry() {
  void load()
}

watch(
  () => props.projectId,
  () => {
    void load()
  },
)

onMounted(() => {
  void load()
})
</script>

<template>
  <div
    class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-b-0 border-line bg-surface shadow-[var(--shadow-card)]"
    data-testid="project-credentials-panel"
  >
    <div v-if="loading" class="flex flex-1 items-center justify-center text-[13px] text-txt3">
      {{ t('common.loading.inProgress') }}
    </div>

    <div v-else-if="loadError" class="flex flex-1 flex-col items-center justify-center gap-2 px-4 text-center">
      <p class="text-[13px] text-err">{{ loadError }}</p>
      <AppButton size="sm" variant="outline" @click="retry">{{ t('common.buttons.retry') }}</AppButton>
    </div>

    <template v-else>
      <header class="flex shrink-0 flex-col gap-4 border-b border-line bg-surface px-4 py-5 sm:flex-row sm:items-start sm:justify-between sm:px-6 lg:px-8">
        <div class="min-w-0 max-w-3xl">
          <div class="flex items-center gap-2">
            <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-accent/35 bg-accent-dim text-accent-2">
              <Icon name="lock" :size="16" aria-hidden="true" />
            </span>
            <div>
              <p class="m-0 text-[11px] font-semibold uppercase tracking-[0.14em] text-accent-2">{{ t('pages.projectDetail.projectCredentials.eyebrow') }}</p>
              <h2 class="m-0 mt-0.5 text-lg font-semibold text-txt">{{ t('pages.projectDetail.projectCredentials.title') }}</h2>
            </div>
          </div>
          <p class="mt-3 max-w-2xl text-[13px] leading-6 text-txt2">{{ t('pages.projectDetail.projectCredentials.subtitle') }}</p>
          <div class="mt-3 flex items-start gap-2 rounded-lg border border-info/30 bg-info/10 px-3 py-2.5 text-[11px] leading-5 text-info">
            <Icon name="help" :size="14" class="mt-0.5 shrink-0" aria-hidden="true" />
            <span>{{ t('pages.projectDetail.projectCredentials.envFallbackHint') }}</span>
          </div>
        </div>
        <AppButton
          variant="primary"
          size="md"
          icon="plus"
          class="shrink-0 self-start sm:mt-1"
          data-testid="project-credential-create"
          @click="openCreate"
        >
          {{ t('pages.projectDetail.projectCredentials.create') }}
        </AppButton>
      </header>

      <div v-if="!orderedItems.length" class="flex flex-1 items-center justify-center px-6 py-12 text-center text-[13px] text-txt3">
        {{ t('pages.projectDetail.projectCredentials.empty') }}
      </div>
      <div v-else class="scroll-area min-h-0 flex-1 overflow-y-auto px-4 py-5 sm:px-6 lg:px-8">
        <div class="mx-auto w-full max-w-[1600px] space-y-6">
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-3" data-testid="project-credentials-summary">
            <div class="rounded-lg border border-line bg-base/45 px-4 py-3">
              <p class="m-0 text-[11px] font-medium uppercase tracking-[0.12em] text-txt3">{{ t('pages.projectDetail.projectCredentials.summary.total') }}</p>
              <p class="mt-1 text-xl font-semibold tabular-nums text-txt">{{ orderedItems.length }}</p>
            </div>
            <div class="rounded-lg border border-line bg-base/45 px-4 py-3">
              <p class="m-0 text-[11px] font-medium uppercase tracking-[0.12em] text-txt3">{{ t('pages.projectDetail.projectCredentials.summary.configured') }}</p>
              <p class="mt-1 text-xl font-semibold tabular-nums text-ok">{{ configuredCount }}<span class="ml-1 text-xs font-normal text-txt3">/ {{ orderedItems.length }}</span></p>
            </div>
            <div class="rounded-lg border border-accent/25 bg-accent-dim/35 px-4 py-3">
              <p class="m-0 text-[11px] font-medium uppercase tracking-[0.12em] text-accent-2">{{ t('pages.projectDetail.projectCredentials.summary.apiKeys') }}</p>
              <p class="mt-1 text-xl font-semibold tabular-nums text-txt">{{ apiKeyCount }}</p>
            </div>
          </div>

          <section
            v-for="group in credentialGroups"
            :key="group.id"
            :data-testid="`project-credential-group-${group.id}`"
          >
            <div class="mb-3 flex items-end justify-between gap-3 border-b border-line pb-2">
              <div class="min-w-0">
                <h3 class="m-0 text-sm font-semibold text-txt">{{ groupTitle(group.id) }}</h3>
                <p class="m-0 mt-1 text-[11px] leading-5 text-txt3">{{ groupHint(group.id) }}</p>
              </div>
              <span class="shrink-0 rounded-full border border-line bg-base px-2 py-0.5 text-[11px] tabular-nums text-txt3">{{ group.items.length }}</span>
            </div>

            <div class="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
              <article
                v-for="item in group.items"
                :key="item.id"
                class="min-w-0 rounded-xl border border-line bg-base/35 p-4 transition hover:border-line-strong hover:bg-base/60"
                data-testid="project-credential-row"
              >
                <div class="flex min-w-0 items-start gap-3">
                  <div
                    class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border text-[10px] font-bold uppercase tracking-tight"
                    :class="item.configured ? 'border-ok/35 bg-ok/10 text-ok' : 'border-line bg-surface text-txt3'"
                    aria-hidden="true"
                  >
                    {{ group.id === 'ai' ? 'AI' : group.id === 'git' ? 'GIT' : group.id === 'ssh' ? 'SSH' : 'KEY' }}
                  </div>
                  <div class="min-w-0 flex-1">
                    <div class="flex flex-wrap items-start justify-between gap-2">
                      <div class="min-w-0">
                        <h4 class="m-0 truncate text-sm font-semibold text-txt" :title="itemLabel(item)">{{ itemLabel(item) }}</h4>
                        <p class="m-0 mt-1 text-[10px] font-medium uppercase tracking-[0.1em] text-txt3">{{ typeLabel(item) }}</p>
                      </div>
                      <span
                        class="shrink-0 rounded-full border px-2 py-0.5 text-[11px]"
                        :class="item.configured ? 'border-ok/40 bg-ok/10 text-ok' : 'border-line text-txt3'"
                        data-testid="project-credential-status"
                      >
                        {{ configuredText(item) }}
                      </span>
                    </div>
                    <p v-if="itemTarget(item)" class="m-0 mt-2 break-all font-mono text-[11px] text-txt3">{{ itemTarget(item) }}</p>
                  </div>
                </div>

                <div v-if="item.masked" class="mt-3 flex min-w-0 items-center justify-between gap-2 rounded-lg border border-line bg-surface px-3 py-2" data-testid="project-credential-masked">
                  <code class="min-w-0 truncate font-mono text-[12px] text-txt2">{{ item.masked }}</code>
                  <span class="shrink-0 text-[10px] uppercase tracking-[0.08em] text-txt3">{{ t('pages.projectDetail.projectCredentials.writeOnly') }}</span>
                </div>

                <p v-if="item.source && item.source !== 'project'" class="mt-3 rounded-lg border border-line bg-surface px-3 py-2 text-[11px] leading-5 text-txt3">
                  {{ t('pages.projectDetail.projectCredentials.managedByAdapter', { source: item.source }) }}
                </p>
                <div v-else class="mt-3 flex min-w-0 flex-col gap-2 sm:flex-row sm:items-start">
                  <textarea
                    v-if="isMultiline(item)"
                    v-model="drafts[item.id]"
                    rows="2"
                    class="min-h-[68px] min-w-0 flex-1 resize-y rounded-lg border border-line bg-surface px-3 py-2 font-mono text-[12px] text-txt outline-none transition placeholder:text-txt3 focus:border-accent focus:ring-2 focus:ring-accent/15"
                    :placeholder="item.configured ? t('pages.projectDetail.projectCredentials.replacePlaceholder') : t('pages.projectDetail.projectCredentials.valuePlaceholder')"
                    :data-testid="`project-credential-input-${item.id}`"
                    autocomplete="off"
                    style="-webkit-text-security: disc;"
                  />
                  <input
                    v-else
                    v-model="drafts[item.id]"
                    type="password"
                    class="min-w-0 flex-1 rounded-lg border border-line bg-surface px-3 py-2 font-mono text-[12px] text-txt outline-none transition placeholder:text-txt3 focus:border-accent focus:ring-2 focus:ring-accent/15"
                    :placeholder="item.configured ? t('pages.projectDetail.projectCredentials.replacePlaceholder') : t('pages.projectDetail.projectCredentials.valuePlaceholder')"
                    :data-testid="`project-credential-input-${item.id}`"
                    autocomplete="new-password"
                  />
                  <div class="flex shrink-0 gap-2 sm:flex-col">
                    <AppButton
                      size="sm"
                      variant="primary"
                      class="flex-1 sm:flex-none"
                      :disabled="!drafts[item.id]?.trim() || !!saving[item.id]"
                      :loading="!!saving[item.id]"
                      :data-testid="`project-credential-save-${item.id}`"
                      @click="save(item)"
                    >
                      {{ t('pages.projectDetail.projectCredentials.save') }}
                    </AppButton>
                    <AppButton
                      v-if="item.configured"
                      size="sm"
                      variant="danger"
                      class="flex-1 sm:flex-none"
                      :disabled="!!clearing[item.id]"
                      :loading="!!clearing[item.id]"
                      :data-testid="`project-credential-clear-${item.id}`"
                      @click="clear(item)"
                    >
                      {{ t('pages.projectDetail.projectCredentials.clear') }}
                    </AppButton>
                  </div>
                </div>
                <p v-if="item.updatedAt" class="m-0 mt-2 text-[10px] text-txt3">
                  {{ t('pages.projectDetail.projectCredentials.updatedAt', { time: fmtTime(item.updatedAt) }) }}
                </p>
              </article>
            </div>
          </section>
        </div>
      </div>
    </template>

    <AppModal
      :open="showCreate"
      :title="t('pages.projectDetail.projectCredentials.createTitle')"
      :width="640"
      :close-on-esc="!creating"
      @close="!creating && (showCreate = false)"
    >
      <div class="space-y-5" data-testid="project-credential-create-form">
        <div class="rounded-lg border border-accent/25 bg-accent-dim/30 px-3.5 py-3 text-[11px] leading-5 text-txt2">
          <p class="m-0 font-medium text-txt">{{ t('pages.projectDetail.projectCredentials.createIntroTitle') }}</p>
          <p class="m-0 mt-1">{{ t('pages.projectDetail.projectCredentials.createIntro') }}</p>
        </div>

        <section>
          <div class="mb-2.5 flex items-baseline justify-between gap-3">
            <h3 class="m-0 text-[12px] font-semibold text-txt">{{ t('pages.projectDetail.projectCredentials.basicSection') }}</h3>
            <span class="text-[10px] uppercase tracking-[0.1em] text-txt3">{{ t('pages.projectDetail.projectCredentials.requiredHint') }}</span>
          </div>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label class="block sm:col-span-2">
              <span class="label">{{ t('pages.projectDetail.projectCredentials.name') }}</span>
              <input v-model="createName" class="input w-full" data-testid="project-credential-create-name" autocomplete="off" :placeholder="t('pages.projectDetail.projectCredentials.namePlaceholder')" />
            </label>
            <label class="block">
              <span class="label">{{ t('pages.projectDetail.projectCredentials.type') }}</span>
              <select v-model="createType" class="input w-full" data-testid="project-credential-create-type">
                <option v-for="kind in credentialTypes" :key="kind" :value="kind">{{ t(credentialTypeKeys[kind]) }}</option>
              </select>
            </label>
            <label class="block">
              <span class="label">{{ t('pages.projectDetail.projectCredentials.provider') }}</span>
              <input v-model="createProvider" class="input w-full" data-testid="project-credential-create-provider" autocomplete="off" :placeholder="t('pages.projectDetail.projectCredentials.providerPlaceholder')" />
            </label>
          </div>
        </section>

        <section class="border-t border-dashed border-line pt-4">
          <div class="mb-2.5">
            <h3 class="m-0 text-[12px] font-semibold text-txt">{{ t('pages.projectDetail.projectCredentials.bindingSection') }}</h3>
            <p class="m-0 mt-1 text-[11px] leading-5 text-txt3">{{ t('pages.projectDetail.projectCredentials.bindingHint') }}</p>
          </div>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label class="block">
              <span class="label">{{ t('pages.projectDetail.projectCredentials.target') }}</span>
              <input v-model="createTarget" class="input w-full" data-testid="project-credential-create-target" autocomplete="off" :placeholder="t('pages.projectDetail.projectCredentials.targetPlaceholder')" />
            </label>
            <label class="block">
              <span class="label">{{ t('pages.projectDetail.projectCredentials.envKey') }}</span>
              <input v-model="createEnvKey" class="input w-full font-mono" data-testid="project-credential-create-env" autocomplete="off" placeholder="MY_API_KEY" />
            </label>
            <label class="block sm:col-span-2">
              <span class="label">{{ t('pages.projectDetail.projectCredentials.fallbackEnvKey') }}</span>
              <input v-model="createFallbackEnvKey" class="input w-full font-mono" data-testid="project-credential-create-fallback-env" autocomplete="off" :placeholder="t('pages.projectDetail.projectCredentials.fallbackPlaceholder')" />
            </label>
          </div>
        </section>

        <section class="border-t border-dashed border-line pt-4">
          <div class="mb-2.5 flex items-baseline justify-between gap-3">
            <h3 class="m-0 text-[12px] font-semibold text-txt">{{ t('pages.projectDetail.projectCredentials.secretSection') }}</h3>
            <span class="rounded-full border border-warn/30 bg-warn/10 px-2 py-0.5 text-[10px] text-warn">{{ t('pages.projectDetail.projectCredentials.writeOnly') }}</span>
          </div>
          <label class="block">
            <span class="label">{{ t('pages.projectDetail.projectCredentials.value') }}</span>
            <textarea v-model="createValue" rows="4" class="input min-h-[96px] w-full resize-y font-mono" data-testid="project-credential-create-value" autocomplete="new-password" :placeholder="t('pages.projectDetail.projectCredentials.valuePlaceholder')" style="-webkit-text-security: disc;" />
          </label>
        </section>
      </div>
      <template #footer>
        <AppButton variant="ghost" :disabled="creating" @click="showCreate = false">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton
          variant="primary"
          :disabled="creating || !createName.trim() || !createValue.trim() || createTargetMissing"
          :loading="creating"
          data-testid="project-credential-create-submit"
          @click="create"
        >
          {{ t('pages.projectDetail.projectCredentials.create') }}
        </AppButton>
      </template>
    </AppModal>
  </div>
</template>
