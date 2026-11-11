import { inject, provide, type ComputedRef, type InjectionKey } from 'vue'
import { useI18n } from 'vue-i18n'

interface EditorFormContext {
  /** Form path the current server error points at (e.g. `goals[1].title`). */
  errorPath: ComputedRef<string | null>
  errorMessage: ComputedRef<string>
  disabled: ComputedRef<boolean>
}

const KEY: InjectionKey<EditorFormContext> = Symbol('artifactEditorForm')

export function provideEditorForm(ctx: EditorFormContext) {
  provide(KEY, ctx)
}

export function useEditorForm(): EditorFormContext | null {
  return inject(KEY, null)
}

/** Label for a schema key; falls back to the raw key for unknown fields. */
export function useFieldLabel() {
  const { t, te } = useI18n()
  return (key: string) => {
    const k = `pages.artifactEditor.fields.${key}`
    return te(k) ? t(k) : key
  }
}

export function joinPath(base: string, key: string | number): string {
  if (typeof key === 'number') return `${base}[${key}]`
  return base ? `${base}.${key}` : key
}
