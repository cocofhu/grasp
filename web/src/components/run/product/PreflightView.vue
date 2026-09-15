<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'

export type PreflightField = {
  name?: string
  label?: string
  value?: string
  verified?: boolean
  verification?: string
  source?: string
  notes?: string
}

export type PreflightDoc = {
  summary?: string
  confirmed?: boolean
  fields?: PreflightField[]
  unresolved?: string[]
}

const props = defineProps<{ doc: PreflightDoc; accent?: string }>()

const { t } = useI18n()
const showRaw = ref(false)

const rawJson = computed(() => {
  try {
    return JSON.stringify(props.doc ?? {}, null, 2)
  } catch {
    return ''
  }
})

function fieldLabel(f: PreflightField): string {
  return (f.label || f.name || '').trim() || '—'
}
</script>

<template>
  <div class="space-y-4" data-testid="preflight-view">
    <div class="flex flex-wrap items-center gap-2">
      <code class="rounded bg-base px-1.5 py-0.5 font-mono text-[11px] text-txt3">{{
        t('pages.product.preflight.filename')
      }}</code>
      <span
        class="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-[10px] font-medium"
        :class="doc.confirmed ? 'bg-ok/15 text-ok' : 'bg-warn/15 text-warn'"
        data-testid="preflight-confirmed"
      >
        <Icon :name="doc.confirmed ? 'check' : 'gate'" :size="11" />
        {{
          doc.confirmed
            ? t('pages.product.preflight.confirmed')
            : t('pages.product.preflight.unconfirmed')
        }}
      </span>
    </div>

    <div v-if="doc.summary" class="rounded-lg border border-line bg-accent-dim/30 p-3">
      <div class="text-[12px] leading-relaxed text-txt2" data-testid="preflight-summary">
        {{ doc.summary }}
      </div>
    </div>

    <section v-if="doc.fields?.length">
      <div class="mb-1.5 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        <Icon name="ci" :size="12" :style="{ color: accent || 'var(--c-n-ci, #2DD4BF)' }" />
        {{ t('pages.product.preflight.fields') }}
      </div>
      <div class="overflow-x-auto rounded-lg border border-line">
        <table class="w-full min-w-[420px] border-collapse text-left text-[12px]">
          <thead>
            <tr class="border-b border-line bg-base/60 text-[10px] uppercase tracking-wider text-txt3">
              <th class="px-2.5 py-1.5 font-medium">{{ t('pages.product.preflight.labelCol') }}</th>
              <th class="px-2.5 py-1.5 font-medium">{{ t('pages.product.preflight.valueCol') }}</th>
              <th class="px-2.5 py-1.5 font-medium">{{ t('pages.product.preflight.verification') }}</th>
              <th class="px-2.5 py-1.5 font-medium">{{ t('pages.product.preflight.source') }}</th>
              <th class="px-2.5 py-1.5 font-medium">{{ t('pages.product.preflight.notes') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(f, i) in doc.fields"
              :key="f.name || i"
              class="border-b border-line/60 last:border-0"
              data-testid="preflight-field-row"
            >
              <td class="px-2.5 py-2 align-top">
                <div class="font-medium text-txt">{{ fieldLabel(f) }}</div>
                <div
                  v-if="f.verified != null"
                  class="mt-0.5 text-[10px]"
                  :class="f.verified ? 'text-ok' : 'text-txt3'"
                >
                  {{
                    f.verified
                      ? t('pages.product.preflight.verified')
                      : t('pages.product.preflight.unverified')
                  }}
                </div>
              </td>
              <td class="px-2.5 py-2 align-top font-mono text-[11px] text-txt whitespace-pre-wrap break-all">
                {{ f.value ?? '' }}
              </td>
              <td class="px-2.5 py-2 align-top text-[11px] text-txt2">{{ f.verification || '—' }}</td>
              <td class="px-2.5 py-2 align-top text-[11px] text-txt2">{{ f.source || '—' }}</td>
              <td class="px-2.5 py-2 align-top text-[11px] text-txt3">{{ f.notes || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="doc.unresolved?.length" class="rounded-lg border border-warn/30 bg-warn/5 p-3">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-warn">
        {{ t('pages.product.preflight.unresolved') }}
      </div>
      <ul class="space-y-1">
        <li
          v-for="(u, i) in doc.unresolved"
          :key="i"
          class="text-[11px] leading-5 text-txt2"
        >
          {{ u }}
        </li>
      </ul>
    </section>

    <section>
      <button
        type="button"
        class="inline-flex items-center gap-1 rounded-md border border-line px-2 py-1 text-[11px] text-txt2 hover:border-line-strong hover:text-txt"
        data-testid="preflight-raw-toggle"
        @click="showRaw = !showRaw"
      >
        <Icon name="doc" :size="12" />
        {{ t('pages.product.preflight.rawJson') }}
        <Icon :name="showRaw ? 'chevron-down' : 'chevron-right'" :size="12" :class="showRaw ? 'rotate-180' : ''" />
      </button>
      <pre
        v-if="showRaw"
        class="mt-2 overflow-x-auto rounded-lg border border-line bg-base/40 p-2.5 font-mono text-[11px] leading-relaxed text-txt2"
        data-testid="preflight-raw-json"
      >{{ rawJson }}</pre>
    </section>
  </div>
</template>
