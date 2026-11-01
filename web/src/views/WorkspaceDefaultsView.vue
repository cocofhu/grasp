<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import WorkspacePage from '@/components/shell/WorkspacePage.vue'
import SettingsStatStrip from '@/components/settings/SettingsStatStrip.vue'
import type { SettingsStatItem } from '@/components/settings/settingsStats'
import { api } from '@/lib/api/api'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import { useToast } from '@/lib/composables/useToast'
import type { WorkspaceVariable } from '@/lib/shared/types'
const { t } = useI18n()
const route = useRoute()
const toast = useToast()
const { workspace } = useWorkspaceContext()
const isVariables = computed(() => route.meta.workspaceTool === 'variables')
const savingVars = ref(false)
const varRows = ref<WorkspaceVariable[]>([])
let workspaceGeneration = 0
watch(workspace, (p, previous) => {
 if (!p || !previous) workspaceGeneration++
 if (!p) {
   varRows.value = []
   savingVars.value = false
   return
 }
 varRows.value = (p.variables || []).map((v) => ({ ...v }))
}, { immediate: true, flush: 'sync' })
const overviewStats = computed<SettingsStatItem[]>(() => [
  { key: 'total', icon: 'variable', label: t('pages.settingsOverview.stats.variablesTotal'), value: varRows.value.length, tone: 'accent' },
  {
    key: 'with-default',
    icon: 'check',
    label: t('pages.settingsOverview.stats.variablesWithDefault'),
    value: varRows.value.filter((row) => row.value !== undefined && row.value !== null && String(row.value) !== '').length,
    total: varRows.value.length,
  },
  { key: 'types', icon: 'layers', label: t('pages.settingsOverview.stats.variablesTypes'), value: new Set(varRows.value.map((row) => row.type || 'string')).size },
])
const VAR_TYPES = computed(() => [
  { value: 'string', label: t('common.varTypes.string') },
  { value: 'paragraph', label: t('common.varTypes.paragraph') },
  { value: 'number', label: t('common.varTypes.number') },
  { value: 'bool', label: t('common.varTypes.bool') },
  { value: 'select', label: t('common.varTypes.select') },
])

async function saveVars() {
  if (!workspace.value) return
  const current = workspaceGeneration
  savingVars.value = true
  try {
    // Submit full row objects (incl. hidden ask/required/editable) so mergeWorkspaceVars
    // whole-table replace does not wipe server-side values.
    const saved = await api.updateWorkspace({
      variables: varRows.value.filter((v) => v.name.trim()),
    })
    if (current !== workspaceGeneration) return
    workspace.value = saved
    varRows.value = (workspace.value.variables || []).map((v) => ({ ...v }))
    toast.success(t('pages.workspace.saved'))
  } catch (e: any) {
    if (current !== workspaceGeneration) return
    toast.error(String(e?.message || e))
  } finally {
    if (current === workspaceGeneration) savingVars.value = false
  }
}

const SECRET_MASK = '****'

function addVarRow() {
  varRows.value.push({
    name: '',
    type: 'string',
    value: '',
    secret: false,
    desc: '',
    options: '',
    ask: false,
    required: false,
    editable: false,
  })
}
function removeVarRow(i: number) {
  varRows.value.splice(i, 1)
}

/** Clear mask when un-secreting or renaming so **** is never treated as plaintext. */
function onVarSecretChange(row: WorkspaceVariable, secret: boolean) {
  row.secret = secret
  if (!secret && row.value === SECRET_MASK) {
    row.value = ''
  }
}
function onVarNameChange(row: WorkspaceVariable, name: string) {
  if (row.name !== name && row.value === SECRET_MASK) {
    row.value = ''
  }
  row.name = name
}

/** In-place type switch — never rebuild the row object (preserves ask/required/editable). */
function onVarTypeChange(row: WorkspaceVariable, type: string) {
  row.type = type
  if (type === 'bool') {
    row.value = row.value === true || row.value === 'true'
  } else if (type === 'number') {
    const n = Number(row.value)
    row.value = Number.isFinite(n) ? n : 0
  } else if (type === 'select') {
    if (row.options == null) row.options = ''
    row.value = row.value == null ? '' : String(row.value)
  } else {
    row.value = row.value == null ? '' : String(row.value)
  }
}

function selectOptions(row: WorkspaceVariable): string[] {
  return String(row.options || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

function isBoolTrue(row: WorkspaceVariable): boolean {
  return row.value === true || row.value === 'true'
}

function setBoolValue(row: WorkspaceVariable, v: boolean) {
  row.value = v
}

function onVarValueInput(row: WorkspaceVariable, raw: string, asNumber = false) {
  row.value = asNumber ? Number(raw) : raw
}


</script>
<template>
<WorkspacePage v-if="isVariables" title-key="nav.variables" description-key="pages.settingsOverview.desc.variables" icon="variable" layout="workspace">
      <SettingsStatStrip class="mb-5 shrink-0" :items="overviewStats" data-testid="workspace-vars-stats" />
      <!-- Variables tab: fill remaining main area; no varsHint / merge-rules row -->
      <div class="workspace-variables flex min-h-0 min-w-0 flex-1 flex-col">
        <!-- Empty: same shell as sandbox tab -->
        <div
          v-if="!varRows.length"
          class="flex min-h-[360px] flex-1 flex-col"
          data-testid="workflow-vars-empty-shell"
        >
          <div class="flex flex-1 flex-col items-center justify-center">
            <EmptyState
              icon="doc"
              :title="t('pages.workspace.varsEmptyTitle')"
              :desc="t('pages.workspace.varsEmptyDesc')"
            >
              <AppButton variant="primary" icon="plus" @click="addVarRow">
                {{ t('pages.workspace.addRow') }}
              </AppButton>
            </EmptyState>
          </div>
        </div>

        <!-- Data: head / scroll rows / foot stick to shell bottom -->
        <div
          v-else
          class="workspace-vars-card flex min-h-0 flex-1 flex-col overflow-hidden"
          data-testid="workflow-vars-data-panel"
        >
          <div
            class="workspace-var-header shrink-0 gap-2 border-b border-line/60 py-2.5 text-[11px] font-semibold uppercase tracking-wider text-txt3"
          >
            <span>{{ t('pages.workspace.colVarName') }}</span>
            <span>{{ t('pages.workspace.colDefault') }}</span>
            <span>{{ t('pages.workspace.colType') }}</span>
            <span>{{ t('common.table.actions') }}</span>
          </div>

          <div class="scroll-area flex min-h-0 flex-1 flex-col overflow-y-auto">
            <div
              v-for="(row, i) in varRows"
              :key="i"
              class="border-b border-line/60 py-3 last:border-b-0"
            >
              <div
                class="workspace-var-fields grid gap-2"
              >
                <label class="workspace-var-name min-w-0">
                  <span class="workspace-var-label">{{ t('pages.workspace.colVarName') }}</span>
                  <input
                    :value="row.name"
                    :aria-label="t('pages.workspace.colVarName')"
                    :data-testid="`workspace-var-name-${i}`"
                    class="input min-w-0 px-2.5 py-1.5 font-mono text-xs"
                    :placeholder="t('pages.workspace.varName')"
                    @input="onVarNameChange(row, ($event.target as HTMLInputElement).value)"
                  />

                </label>
                <div class="workspace-var-value min-w-0">
                  <span class="workspace-var-label">{{ t('pages.workspace.colDefault') }}</span>
                  <!-- Value control matrix by type -->
                  <div
                    v-if="row.type === 'bool'"
                    role="group"
                    :aria-label="t('pages.workspace.colDefault')"
                    class="rounded-lg flex min-w-0 overflow-hidden border border-line"
                  >
                    <button
                      type="button"
                      class="flex-1 bg-base px-2.5 py-1.5 text-xs transition"
                      :class="isBoolTrue(row) ? 'bg-accent-dim text-accent-2' : 'text-txt3'"
                      @click="setBoolValue(row, true)"
                    >
                      true
                    </button>
                    <button
                      type="button"
                      class="flex-1 bg-base px-2.5 py-1.5 text-xs transition"
                      :class="!isBoolTrue(row) ? 'bg-accent-dim text-accent-2' : 'text-txt3'"
                      @click="setBoolValue(row, false)"
                    >
                      false
                    </button>
                  </div>
                  <input
                    v-else-if="row.type === 'number'"
                    :aria-label="t('pages.workspace.colDefault')"
                    :value="row.value == null ? '' : String(row.value)"
                    type="number"
                    class="input w-full min-w-0 px-2.5 py-1.5 text-xs"
                    :placeholder="t('pages.workspace.varValue')"
                    @input="onVarValueInput(row, ($event.target as HTMLInputElement).value, true)"
                  />
                  <textarea
                    v-else-if="row.type === 'paragraph'"
                    :aria-label="t('pages.workspace.colDefault')"
                    :value="row.value == null ? '' : String(row.value)"
                    rows="2"
                    class="input w-full min-h-[40px] min-w-0 px-2.5 py-1.5 text-xs"
                    :placeholder="t('pages.workspace.varValue')"
                    @input="onVarValueInput(row, ($event.target as HTMLTextAreaElement).value)"
                  />
                  <select
                    v-else-if="row.type === 'select'"
                    :aria-label="t('pages.workspace.colDefault')"
                    :value="row.value == null ? '' : String(row.value)"
                    class="input w-full min-w-0 px-2.5 py-1.5 text-xs"
                    @change="onVarValueInput(row, ($event.target as HTMLSelectElement).value)"
                  >
                    <option value="">{{ t('pages.workspace.selectPlaceholder') }}</option>
                    <option v-for="opt in selectOptions(row)" :key="opt" :value="opt">{{ opt }}</option>
                  </select>
                  <input
                    v-else
                    :aria-label="t('pages.workspace.colDefault')"
                    :value="row.value == null ? '' : String(row.value)"
                    class="input w-full min-w-0 px-2.5 py-1.5 text-xs"
                    :placeholder="t('pages.workspace.varValue')"
                    :type="row.secret ? 'password' : 'text'"
                    @input="onVarValueInput(row, ($event.target as HTMLInputElement).value)"
                  />

                </div>
                <div class="workspace-var-type min-w-0">
                  <label class="workspace-var-label" :for="`workspace-var-type-${i}`">{{ t('pages.workspace.colType') }}</label>
                  <div class="flex min-w-0 items-center gap-1.5">
                    <select
                      :id="`workspace-var-type-${i}`"
                      :aria-label="t('pages.workspace.colType')"
                      :value="row.type"
                      class="input min-w-0 flex-1 px-2.5 py-1.5 text-xs"
                      @change="onVarTypeChange(row, ($event.target as HTMLSelectElement).value)"
                    >
                      <option v-for="vt in VAR_TYPES" :key="vt.value" :value="vt.value">
                        {{ vt.label }}
                      </option>
                    </select>
                    <button
                      type="button"
                      class="chip shrink-0"
                      :class="row.secret ? 'border-accent/50 text-accent-2' : 'text-txt3'"
                      :title="row.secret ? t('pages.workspace.secret') : t('pages.workspace.plain')"
                      @click="onVarSecretChange(row, !row.secret)"
                    >
                      {{ row.secret ? t('pages.workspace.secret') : t('pages.workspace.plain') }}
                    </button>
                </div>
                </div>

                <button
                  type="button"
                  class="workspace-var-delete inline-flex h-7 w-7 shrink-0 items-center justify-center justify-self-end text-txt3 hover:text-err"
                  :title="t('common.buttons.delete')"
                  :aria-label="t('common.buttons.delete')"
                  @click="removeVarRow(i)"
                >
                  <Icon name="close" :size="14" />
                </button>
              </div>

              <!-- Secondary row: desc / select options -->
              <div class="mt-2 flex flex-col gap-1.5">
                <input
                  v-model="row.desc"
                  class="input px-2.5 py-1.5 text-xs"
                  :placeholder="t('pages.workspace.varDescPlaceholder')"
                />
                <input
                  v-if="row.type === 'select'"
                  v-model="row.options"
                  class="input px-2.5 py-1.5 font-mono text-xs"
                  :placeholder="t('pages.workspace.varOptionsPlaceholder')"
                />
              </div>
            </div>
          </div>

          <div
            class="flex shrink-0 flex-wrap gap-2 border-t border-line/60 py-3"
            data-testid="workflow-vars-footer"
          >
            <AppButton variant="outline" icon="plus" @click="addVarRow">
              {{ t('pages.workspace.addRow') }}
            </AppButton>
            <AppButton variant="primary" :disabled="savingVars" @click="saveVars">
              {{ savingVars ? t('common.buttons.saving') : t('common.buttons.save') }}
            </AppButton>
          </div>
        </div>
      </div>

</WorkspacePage>
</template>

<style scoped>
.workspace-variables { container: workspace-variables / inline-size; }
.workspace-var-header, .workspace-var-fields { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr) 180px 32px; align-items: center; }
.workspace-vars-card { border: 1px solid rgb(var(--c-line)); border-radius: 12px; padding-inline: 20px; background: rgb(var(--c-surface)); }
@media (max-width: 767px) { .workspace-vars-card { padding-inline: 16px; } }
.workspace-var-label { display: none; margin-bottom: 6px; color: rgb(var(--c-txt3)); font-size: 12px; line-height: 18px; }
.workspace-var-name > input { width: 100%; }
@container workspace-variables (max-width: 799px) {
  .workspace-var-header { display: none; }
  .workspace-var-fields { grid-template-columns: minmax(0, 1fr) 28px; align-items: end; gap: 12px 8px; }
  .workspace-var-name, .workspace-var-value { grid-column: 1 / -1; }
  .workspace-var-type { grid-column: 1; }
  .workspace-var-delete { grid-column: 2; margin-bottom: 2px; }
  .workspace-var-label { display: block; }
}
</style>
