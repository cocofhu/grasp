<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import AppButton from '../../ui/AppButton.vue'

const props = defineProps<{ config: Record<string, any> }>()
const { t } = useI18n()

const VAR_SYNTAX = '{' + '{vars.名称}' + '}'
const VAR_TYPES = computed(() =>
  ['string', 'paragraph', 'number', 'bool', 'select', 'repos'].map((value) => ({ value, label: t(`common.varTypes.${value}`) })),
)

watch(
  () => props.config,
  (c) => {
    if (!Array.isArray(c.variables)) c.variables = []
  },
  { immediate: true },
)
const vars = computed<any[]>(() => props.config.variables)

function addVariable() {
  vars.value.push({ name: '', type: 'string', value: '', ask: false, desc: '', required: false, editable: true })
}

function asRepos(v: any): any[] {
  if (!Array.isArray(v.value)) {
    let parsed: unknown = []
    if (typeof v.value === 'string' && v.value.trim()) {
      try {
        parsed = JSON.parse(v.value)
      } catch {
        parsed = []
      }
    }
    v.value = Array.isArray(parsed) ? parsed : []
  }
  return v.value
}
</script>

<template>
  <div class="space-y-2" data-testid="field-variables">
    <div v-for="(v, i) in vars" :key="i" class="space-y-1.5 rounded-md border border-line bg-surface p-2.5">
      <div class="flex items-center gap-2">
        <input v-model="v.name" class="input w-32 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.variables.namePlaceholder')" />
        <select v-model="v.type" class="input flex-1 text-[12px]">
          <option v-for="vt in VAR_TYPES" :key="vt.value" :value="vt.value">{{ vt.label }}</option>
        </select>
        <button type="button" class="text-txt3 hover:text-err" :aria-label="t('common.buttons.delete')" @click="vars.splice(i, 1)"><Icon name="close" :size="14" /></button>
      </div>
      <input v-if="v.ask" v-model="v.desc" class="input text-[12px]" :placeholder="t('pages.workflowEditor.inspector.variables.descPlaceholder')" />
      <input v-if="v.type === 'select'" v-model="v.options" class="input text-[12px]" :placeholder="t('pages.workflowEditor.inspector.variables.optionsPlaceholder')" />
      <select v-else-if="v.type === 'bool'" v-model="v.value" class="input text-[12px]">
        <option :value="true">true</option>
        <option :value="false">false</option>
      </select>
      <input
        v-else-if="v.type === 'number'"
        v-model.number="v.value"
        type="number"
        class="input text-[12px]"
        :placeholder="v.ask ? t('pages.workflowEditor.inspector.variables.defaultOptional') : t('pages.workflowEditor.inspector.variables.initialValue')"
      />
      <div v-else-if="v.type === 'repos'" class="space-y-2">
        <div v-for="(r, ri) in asRepos(v)" :key="ri" class="space-y-1.5 rounded-md border border-line bg-base/40 p-2">
          <div class="flex items-center gap-1.5">
            <span class="flex items-center gap-1 text-[11px] font-medium text-txt2"><Icon name="git" :size="12" />{{ t('pages.workflowEditor.inspector.repos.itemLabel', { n: ri + 1 }) }}</span>
            <button type="button" class="ml-auto shrink-0 text-txt3 hover:text-err" :title="t('pages.workflowEditor.inspector.repos.remove')" @click="asRepos(v).splice(ri, 1)"><Icon name="close" :size="14" /></button>
          </div>
          <input v-model="r.url" class="input w-full font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.repos.urlPlaceholder')" />
          <div class="flex items-center gap-1.5">
            <input v-model="r.name" class="input flex-1 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.repos.namePlaceholder')" />
            <input v-model="r.branch" class="input flex-1 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.repos.branchPlaceholder')" />
          </div>
        </div>
        <AppButton size="sm" variant="subtle" icon="plus" @click="asRepos(v).push({ name: '', url: '', branch: '' })">{{ t('pages.workflowEditor.inspector.repos.add') }}</AppButton>
      </div>
      <textarea
        v-else
        v-model="v.value"
        class="input min-h-[40px] text-[12px]"
        :placeholder="v.ask ? t('pages.workflowEditor.inspector.variables.defaultOptional') : t('pages.workflowEditor.inspector.variables.initialValue')"
      />
      <div class="flex flex-wrap items-center gap-1.5">
        <button type="button" class="chip whitespace-nowrap" :class="v.ask ? 'border-accent/50 text-accent-2' : 'text-txt3'" :title="t('pages.workflowEditor.inspector.variables.askTitle')" @click="v.ask = !v.ask">
          <Icon name="play" :size="12" />{{ t('pages.workflowEditor.inspector.variables.askAtLaunch') }}
        </button>
        <template v-if="v.ask">
          <button type="button" class="chip whitespace-nowrap" :class="v.required ? 'border-accent/50 text-accent-2' : 'text-txt3'" @click="v.required = !v.required">
            <Icon name="check" :size="12" />{{ t('common.required') }}
          </button>
          <button
            type="button"
            class="chip whitespace-nowrap"
            :class="v.editable !== false ? 'border-accent/50 text-accent-2' : 'text-txt3'"
            :title="v.editable !== false ? t('pages.workflowEditor.inspector.variables.editableTitle') : t('pages.workflowEditor.inspector.variables.lockedTitle')"
            @click="v.editable = v.editable === false"
          >
            <Icon :name="v.editable !== false ? 'edit' : 'gate'" :size="12" />{{ v.editable !== false ? t('pages.workflowEditor.inspector.variables.editable') : t('pages.workflowEditor.inspector.variables.locked') }}
          </button>
        </template>
      </div>
    </div>
    <AppButton size="sm" variant="subtle" icon="plus" @click="addVariable">{{ t('pages.workflowEditor.inspector.variables.add') }}</AppButton>
    <p class="text-[10.5px] leading-4 text-txt3" v-html="t('pages.workflowEditor.inspector.variables.help', { syntax: VAR_SYNTAX })" />
  </div>
</template>
