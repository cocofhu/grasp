import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { NODE_DEFS as RAW_NODE_DEFS } from '@/data/nodeRegistry'
import type { NodeType, NodeTypeDef } from '@/lib/shared/types'

function safeT(t: (key: string) => string, key: string | undefined): string | undefined {
  if (!key) return undefined
  try {
    return t(key)
  } catch {
    // Malformed ICU / nested `{{...}}` in locale strings must not blank the canvas.
    return key
  }
}

function translateNodeDef(t: (key: string) => string, raw: NodeTypeDef): NodeTypeDef {
  return {
    ...raw,
    label: safeT(t, raw.label) ?? raw.label,
    desc: safeT(t, raw.desc) ?? raw.desc,
    category: safeT(t, raw.category) ?? raw.category,
    help: safeT(t, raw.help),
    fields: raw.fields.map((f) => ({
      ...f,
      label: safeT(t, f.label) ?? f.label,
      help: safeT(t, f.help),
      placeholder: safeT(t, f.placeholder),
      options: f.options?.map((o) => ({
        ...o,
        label: o.label.startsWith('nodes.') || o.label.includes('.')
          ? (safeT(t, o.label) ?? o.label)
          : o.label,
      })),
    })),
    outputs: raw.outputs.map((o) => ({
      ...o,
      desc: safeT(t, o.desc) ?? o.desc,
    })),
    defaults: raw.defaults,
  }
}

export function useNodeDefs() {
  const { t, locale } = useI18n()

  const NODE_DEFS = computed(() => {
    void locale.value
    const out = {} as Record<NodeType, NodeTypeDef>
    for (const type of Object.keys(RAW_NODE_DEFS) as NodeType[]) {
      out[type] = translateNodeDef(t, RAW_NODE_DEFS[type])
    }
    return out
  })

  return { NODE_DEFS }
}
