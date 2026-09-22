<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import AnnotateBtn from './AnnotateBtn.vue'

export type FuncReq = {
  id?: string
  title?: string
  detail?: string
  priority?: string
  acceptance_criteria?: string[]
  scenario_ids?: string[]
}
export type NonFuncReq = { id?: string; category?: string; detail?: string; metric?: string }
export type Persona = { id?: string; name?: string; description?: string; goals?: string[] }
export type UserScenario = {
  id?: string
  name?: string
  actor?: string
  trigger?: string
  flow?: string
  outcome?: string
}
export type ExtInterface = {
  id?: string
  name?: string
  kind?: string
  direction?: string
  description?: string
}
export type DataEntity = { id?: string; name?: string; description?: string; attributes?: string[] }
export type ReqRisk = { id?: string; description?: string; mitigation?: string }
export type GlossaryEntry = { term?: string; definition?: string }

export type ClarifiedRequirementDoc = {
  title?: string
  summary?: string
  background?: string
  work_kind?: string
  goals?: string[]
  success_metrics?: string[]
  in_scope?: string[]
  out_of_scope?: string[]
  personas?: Persona[]
  user_scenarios?: UserScenario[]
  functional_requirements?: FuncReq[]
  non_functional_requirements?: NonFuncReq[]
  external_interfaces?: ExtInterface[]
  data_entities?: DataEntity[]
  business_rules?: string[]
  edge_cases?: string[]
  assumptions?: string[]
  dependencies?: string[]
  constraints?: string[]
  limitations?: string[]
  risks?: ReqRisk[]
  glossary?: GlossaryEntry[]
  open_questions?: string[]
}

defineProps<{ doc: ClarifiedRequirementDoc; accent?: string }>()

const { t } = useI18n()
</script>

<template>
  <div class="space-y-4">
    <div v-if="doc.summary" class="group rounded-lg border border-line bg-accent-dim/30 p-3">
      <div v-if="doc.title" class="mb-1 flex items-center gap-1 text-[13px] font-semibold text-txt">
        <span data-json-path="title" :data-label="doc.title">{{ doc.title }}</span>
        <AnnotateBtn json-path="title" :label="doc.title" />
      </div>
      <div class="flex items-start gap-1 text-[12px] leading-relaxed text-txt2">
        <span class="min-w-0 flex-1" data-json-path="summary" data-label="概述">{{ doc.summary }}</span>
        <AnnotateBtn json-path="summary" label="概述" />
      </div>
      <div v-if="doc.work_kind" class="mt-2 flex items-center gap-1.5 text-[11px] text-txt3">
        <span class="font-semibold uppercase tracking-wider">{{ t('pages.product.clarifiedRequirement.workKind') }}</span>
        <code
          class="rounded bg-base px-1.5 py-0.5 font-mono text-[10px] text-txt2"
          data-json-path="work_kind"
          :data-label="t('pages.product.clarifiedRequirement.workKind')"
        >{{ doc.work_kind }}</code>
        <AnnotateBtn json-path="work_kind" :label="t('pages.product.clarifiedRequirement.workKind')" />
      </div>
    </div>

    <section v-if="doc.background" class="group">
      <div class="mb-1.5 flex items-center gap-1 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('pages.product.clarifiedRequirement.background') }}
        <AnnotateBtn json-path="background" :label="t('pages.product.clarifiedRequirement.background')" />
      </div>
      <div
        class="text-[11px] leading-relaxed text-txt2"
        data-json-path="background"
        :data-label="t('pages.product.clarifiedRequirement.background')"
      >{{ doc.background }}</div>
    </section>

    <section v-if="doc.goals?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.goals') }}</div>
      <ul class="space-y-1">
        <li v-for="(g, i) in doc.goals" :key="i" class="group flex items-start gap-1.5 text-[11px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">•</span>
          <span class="min-w-0 flex-1" :data-json-path="`goals[${i}]`" :data-label="`目标 ${i + 1}`">{{ g }}</span>
          <AnnotateBtn :json-path="`goals[${i}]`" :label="`目标 ${i + 1}`" />
        </li>
      </ul>
    </section>

    <section v-if="doc.success_metrics?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.successMetrics') }}</div>
      <ul class="space-y-1">
        <li v-for="(m, i) in doc.success_metrics" :key="i" class="group flex items-start gap-1.5 text-[11px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">•</span>
          <span class="min-w-0 flex-1" :data-json-path="`success_metrics[${i}]`" :data-label="m">{{ m }}</span>
          <AnnotateBtn :json-path="`success_metrics[${i}]`" :label="m" />
        </li>
      </ul>
    </section>

    <section v-if="doc.in_scope?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.inScope') }}</div>
      <div class="flex flex-wrap gap-1.5">
        <span v-for="(s, i) in doc.in_scope" :key="i" class="group inline-flex items-center rounded-md bg-ok/10 px-2 py-0.5 text-[11px] text-txt2" :data-json-path="`in_scope[${i}]`" :data-label="s">{{ s }}<AnnotateBtn :json-path="`in_scope[${i}]`" :label="s" /></span>
      </div>
    </section>

    <section v-if="doc.out_of_scope?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.outOfScope') }}</div>
      <div class="flex flex-wrap gap-1.5">
        <span v-for="(o, i) in doc.out_of_scope" :key="i" class="group inline-flex items-center rounded-md bg-base px-2 py-0.5 text-[11px] text-txt3 line-through" :data-json-path="`out_of_scope[${i}]`" :data-label="o">
          {{ o }}
          <AnnotateBtn :json-path="`out_of_scope[${i}]`" :label="o" />
        </span>
      </div>
    </section>

    <section v-if="doc.personas?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.personas') }}</div>
      <div class="space-y-2">
        <div v-for="(p, i) in doc.personas" :key="p.id || i" class="group rounded-lg border border-line bg-base/40 p-2.5">
          <div class="flex flex-wrap items-center gap-2">
            <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-[10px] text-txt3">{{ p.id }}</code>
            <span
              class="text-[12px] font-semibold text-txt"
              :data-json-path="`personas[${p.id || i}].name`"
              :data-label="p.name || `画像 ${i + 1}`"
            >{{ p.name }}</span>
            <AnnotateBtn :json-path="`personas[${p.id || i}]`" :label="p.name || `画像 ${i + 1}`" />
          </div>
          <div
            v-if="p.description"
            class="mt-1 text-[11px] leading-relaxed text-txt3"
            :data-json-path="`personas[${p.id || i}].description`"
            :data-label="`${p.name || i} 描述`"
          >{{ p.description }}</div>
          <ul v-if="p.goals?.length" class="mt-1 space-y-0.5">
            <li
              v-for="(g, k) in p.goals"
              :key="k"
              class="text-[11px] text-txt2"
              :data-json-path="`personas[${p.id || i}].goals[${k}]`"
              :data-label="`${p.name || i} 目标 ${k + 1}`"
            >• {{ g }}</li>
          </ul>
        </div>
      </div>
    </section>

    <section v-if="doc.user_scenarios?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.scenarios') }}</div>
      <div class="space-y-2">
        <div v-for="(s, i) in doc.user_scenarios" :key="s.id || i" class="group rounded-lg border border-line bg-base/40 p-2.5 text-[11px] leading-5 text-txt2">
          <div class="flex flex-wrap items-center gap-2">
            <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-[10px] text-txt3">{{ s.id }}</code>
            <span class="text-[12px] font-semibold text-txt">{{ s.name }}</span>
            <AnnotateBtn :json-path="`user_scenarios[${s.id || i}]`" :label="s.name || `场景 ${i + 1}`" />
          </div>
          <div
            v-if="s.actor"
            class="mt-1 text-txt3"
            :data-json-path="`user_scenarios[${s.id || i}].actor`"
            :data-label="`${s.name || i} actor`"
          >{{ s.actor }}</div>
          <div
            v-if="s.trigger"
            class="mt-0.5"
            :data-json-path="`user_scenarios[${s.id || i}].trigger`"
            :data-label="`${s.name || i} trigger`"
          >{{ s.trigger }}</div>
          <div
            v-if="s.flow"
            class="mt-0.5"
            :data-json-path="`user_scenarios[${s.id || i}].flow`"
            :data-label="`${s.name || i} flow`"
          >{{ s.flow }}</div>
          <div
            v-if="s.outcome"
            class="mt-0.5 text-txt"
            :data-json-path="`user_scenarios[${s.id || i}].outcome`"
            :data-label="`${s.name || i} outcome`"
          >{{ s.outcome }}</div>
        </div>
      </div>
    </section>

    <section v-if="doc.functional_requirements?.length">
      <div class="mb-1.5 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        <Icon name="check" :size="12" :style="{ color: accent }" />{{ t('pages.product.clarifiedRequirement.functional', { n: doc.functional_requirements.length }) }}
      </div>
      <div class="space-y-2">
        <div v-for="(f, i) in doc.functional_requirements" :key="f.id || i" class="group rounded-lg border border-line bg-base/40 p-2.5">
          <div class="flex flex-wrap items-center gap-2">
            <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-[10px] text-txt3">{{ f.id }}</code>
            <span class="text-[12px] font-semibold text-txt">{{ f.title }}</span>
            <span v-if="f.priority" class="rounded bg-elevated px-1.5 py-0.5 text-[9px] font-medium uppercase text-txt2">{{ f.priority }}</span>
            <AnnotateBtn :json-path="`functional_requirements[${f.id || i}]`" :label="f.title || `功能需求 ${i + 1}`" />
          </div>
          <div
            v-if="f.detail"
            class="mt-1 text-[11px] leading-relaxed text-txt3"
            :data-json-path="`functional_requirements[${f.id || i}].detail`"
            :data-label="`${f.title || i} 详情`"
          >{{ f.detail }}</div>
          <div
            v-if="f.scenario_ids?.length"
            class="mt-1 text-[10px] text-txt3"
            :data-json-path="`functional_requirements[${f.id || i}].scenario_ids`"
            :data-label="`${f.title || i} 场景`"
          >{{ f.scenario_ids.join(', ') }}</div>
          <div v-if="f.acceptance_criteria?.length" class="mt-1.5 space-y-1 border-l-2 border-line pl-2.5">
            <div
              v-for="(ac, k) in f.acceptance_criteria"
              :key="k"
              class="flex items-start gap-1.5 text-[11px] leading-5 text-txt2"
              :data-json-path="`functional_requirements[${f.id || i}].acceptance_criteria[${k}]`"
              :data-label="`验收 ${k + 1}`"
            >
              <span class="mt-0.5 shrink-0 rounded bg-ok/10 px-1 text-[9px] font-medium text-ok">{{ t('pages.product.clarifiedRequirement.acceptance') }}</span>{{ ac }}
            </div>
          </div>
        </div>
      </div>
    </section>

    <section v-if="doc.non_functional_requirements?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.nonFunctional') }}</div>
      <div class="space-y-1.5">
        <div
          v-for="(n, i) in doc.non_functional_requirements"
          :key="n.id || i"
          class="group rounded-lg border border-line bg-base/40 p-2.5"
          :data-json-path="`non_functional_requirements[${n.id || i}]`"
          :data-label="n.detail || `NFR ${i + 1}`"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span
              v-if="n.category"
              class="shrink-0 rounded-md bg-elevated px-1.5 py-0.5 text-[10px] font-medium text-txt2"
            >{{ n.category }}</span>
            <AnnotateBtn :json-path="`non_functional_requirements[${n.id || i}]`" :label="n.detail || `NFR ${i + 1}`" />
          </div>
          <div
            v-if="n.detail"
            class="mt-1 min-w-0 w-full text-[11px] leading-relaxed text-txt2 [overflow-wrap:anywhere] break-words"
            :data-json-path="`non_functional_requirements[${n.id || i}]`"
            :data-label="n.detail || `NFR ${i + 1}`"
          >{{ n.detail }}</div>
          <div
            v-if="n.metric"
            class="mt-1 min-w-0 w-full text-[10px] text-txt3 [overflow-wrap:anywhere] break-words"
            :data-json-path="`non_functional_requirements[${n.id || i}].metric`"
            :data-label="`${n.detail || `NFR ${i + 1}`} metric`"
          >{{ t('pages.product.clarifiedRequirement.metric') }}: {{ n.metric }}</div>
        </div>
      </div>
    </section>

    <section v-if="doc.external_interfaces?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.interfaces') }}</div>
      <div class="space-y-1.5">
        <div v-for="(iface, i) in doc.external_interfaces" :key="iface.id || i" class="group rounded-lg border border-line bg-base/40 p-2.5 text-[11px] text-txt2">
          <div class="flex flex-wrap items-center gap-2">
            <code class="rounded bg-base px-1.5 py-0.5 font-mono text-[10px] text-txt3">{{ iface.id }}</code>
            <span class="font-semibold text-txt">{{ iface.name }}</span>
            <span v-if="iface.kind" class="text-[10px] text-txt3">{{ iface.kind }}</span>
            <span v-if="iface.direction" class="text-[10px] text-txt3">{{ iface.direction }}</span>
            <AnnotateBtn :json-path="`external_interfaces[${iface.id || i}]`" :label="iface.name || `接口 ${i + 1}`" />
          </div>
          <div v-if="iface.description" class="mt-1 text-txt3">{{ iface.description }}</div>
        </div>
      </div>
    </section>

    <section v-if="doc.data_entities?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.dataEntities') }}</div>
      <div class="space-y-1.5">
        <div v-for="(d, i) in doc.data_entities" :key="d.id || i" class="group rounded-lg border border-line bg-base/40 p-2.5 text-[11px] text-txt2">
          <div class="flex flex-wrap items-center gap-2">
            <code class="rounded bg-base px-1.5 py-0.5 font-mono text-[10px] text-txt3">{{ d.id }}</code>
            <span class="font-semibold text-txt">{{ d.name }}</span>
            <AnnotateBtn :json-path="`data_entities[${d.id || i}]`" :label="d.name || `实体 ${i + 1}`" />
          </div>
          <div v-if="d.description" class="mt-1 text-txt3">{{ d.description }}</div>
          <div v-if="d.attributes?.length" class="mt-1 flex flex-wrap gap-1">
            <span v-for="(a, k) in d.attributes" :key="k" class="rounded bg-base px-1.5 py-0.5 text-[10px] text-txt3">{{ a }}</span>
          </div>
        </div>
      </div>
    </section>

    <section v-if="doc.business_rules?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.businessRules') }}</div>
      <ul class="space-y-1">
        <li v-for="(r, i) in doc.business_rules" :key="i" class="group flex items-start gap-1.5 text-[11px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">•</span>
          <span class="min-w-0 flex-1" :data-json-path="`business_rules[${i}]`" :data-label="r">{{ r }}</span>
          <AnnotateBtn :json-path="`business_rules[${i}]`" :label="r" />
        </li>
      </ul>
    </section>

    <section v-if="doc.edge_cases?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.edgeCases') }}</div>
      <ul class="space-y-1">
        <li v-for="(e, i) in doc.edge_cases" :key="i" class="group flex items-start gap-1.5 text-[11px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">•</span>
          <span class="min-w-0 flex-1" :data-json-path="`edge_cases[${i}]`" :data-label="e">{{ e }}</span>
          <AnnotateBtn :json-path="`edge_cases[${i}]`" :label="e" />
        </li>
      </ul>
    </section>

    <section v-if="doc.assumptions?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.assumptions') }}</div>
      <ul class="space-y-1">
        <li v-for="(a, i) in doc.assumptions" :key="i" class="group flex items-start gap-1.5 text-[11px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">•</span>
          <span class="min-w-0 flex-1" :data-json-path="`assumptions[${i}]`" :data-label="a">{{ a }}</span>
          <AnnotateBtn :json-path="`assumptions[${i}]`" :label="a" />
        </li>
      </ul>
    </section>

    <section v-if="doc.dependencies?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.dependencies') }}</div>
      <ul class="space-y-1">
        <li v-for="(d, i) in doc.dependencies" :key="i" class="group flex items-start gap-1.5 text-[11px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">•</span>
          <span class="min-w-0 flex-1" :data-json-path="`dependencies[${i}]`" :data-label="d">{{ d }}</span>
          <AnnotateBtn :json-path="`dependencies[${i}]`" :label="d" />
        </li>
      </ul>
    </section>

    <section v-if="doc.constraints?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.constraints') }}</div>
      <ul class="space-y-1">
        <li v-for="(c, i) in doc.constraints" :key="i" class="group flex items-start gap-1.5 text-[11px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">•</span>
          <span class="min-w-0 flex-1" :data-json-path="`constraints[${i}]`" :data-label="c">{{ c }}</span>
          <AnnotateBtn :json-path="`constraints[${i}]`" :label="c" />
        </li>
      </ul>
    </section>

    <section v-if="doc.limitations?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.limitations') }}</div>
      <ul class="space-y-1">
        <li v-for="(l, i) in doc.limitations" :key="i" class="group flex items-start gap-1.5 text-[11px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">•</span>
          <span class="min-w-0 flex-1" :data-json-path="`limitations[${i}]`" :data-label="l">{{ l }}</span>
          <AnnotateBtn :json-path="`limitations[${i}]`" :label="l" />
        </li>
      </ul>
    </section>

    <section v-if="doc.risks?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.risks') }}</div>
      <div class="space-y-1.5">
        <div v-for="(r, i) in doc.risks" :key="r.id || i" class="group rounded-lg border border-line bg-base/40 p-2.5 text-[11px] text-txt2">
          <div class="flex gap-2">
            <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-[10px] text-txt3">{{ r.id }}</code>
            <span class="min-w-0 flex-1" :data-json-path="`risks[${r.id || i}]`" :data-label="r.description || `风险 ${i + 1}`">{{ r.description }}</span>
            <AnnotateBtn :json-path="`risks[${r.id || i}]`" :label="r.description || `风险 ${i + 1}`" />
          </div>
          <div v-if="r.mitigation" class="mt-1 text-txt3" :data-json-path="`risks[${r.id || i}].mitigation`" :data-label="`缓解 ${i + 1}`">{{ r.mitigation }}</div>
        </div>
      </div>
    </section>

    <section v-if="doc.glossary?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.clarifiedRequirement.glossary') }}</div>
      <ul class="space-y-1">
        <li v-for="(g, i) in doc.glossary" :key="i" class="group flex items-start gap-1.5 text-[11px] leading-5 text-txt2">
          <span class="min-w-0 flex-1" :data-json-path="`glossary[${i}]`" :data-label="g.term || `术语 ${i + 1}`"><span class="font-semibold text-txt">{{ g.term }}</span>: {{ g.definition }}</span>
          <AnnotateBtn :json-path="`glossary[${i}]`" :label="g.term || `术语 ${i + 1}`" />
        </li>
      </ul>
    </section>

    <section v-if="doc.open_questions?.length">
      <div class="mb-1.5 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-warn">
        <Icon name="gate" :size="12" />{{ t('pages.product.clarifiedRequirement.openQuestions') }}
      </div>
      <ul class="space-y-1">
        <li v-for="(q, i) in doc.open_questions" :key="i" class="group flex items-start gap-1.5 rounded-md bg-warn/5 px-2 py-1 text-[11px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-warn">?</span>
          <span class="min-w-0 flex-1" :data-json-path="`open_questions[${i}]`" :data-label="q">{{ q }}</span>
          <AnnotateBtn :json-path="`open_questions[${i}]`" :label="q" />
        </li>
      </ul>
    </section>
  </div>
</template>
